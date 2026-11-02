package notify

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Push delivers to every signed-in device of the account.
//
// Delivery.Destination is the user id (see destinationFor); the tokens live on
// the sessions, one per device. Android tokens go to FCM and iOS tokens to
// APNs directly, so the iOS app needs no Firebase SDK. A token either service
// reports as dead is cleared so it is not tried again.
type Push struct {
	Pool *pgxpool.Pool
	FCM  *FCM
	APNs *APNs
}

// ErrDeadToken means the device uninstalled the app or the token rotated.
var ErrDeadToken = errors.New("notify: push token no longer valid")

type pushMessage struct {
	Title string
	Body  string
	Link  string
}

func (Push) Name() string { return "push" }

func (p Push) Send(ctx context.Context, d Delivery) (string, error) {
	rows, err := p.Pool.Query(ctx, `
		SELECT id::text, push_token, coalesce(device_platform, '')
		  FROM sessions
		 WHERE user_id = $1::uuid AND revoked_at IS NULL AND expires_at > now()
		   AND push_token IS NOT NULL AND push_token <> ''`, d.Destination)
	if err != nil {
		return "", fmt.Errorf("push: load devices: %w", err)
	}
	type device struct{ session, token, platform string }
	var devices []device
	for rows.Next() {
		var dv device
		if err := rows.Scan(&dv.session, &dv.token, &dv.platform); err != nil {
			rows.Close()
			return "", fmt.Errorf("push: load devices: %w", err)
		}
		devices = append(devices, dv)
	}
	rows.Close()
	if len(devices) == 0 {
		return "", fmt.Errorf("%w: no device registered for push", ErrPermanent)
	}

	title := d.Subject
	if title == "" {
		title = appName(d.Locale)
	}
	msg := pushMessage{Title: title, Body: d.Body, Link: d.Params["link"]}

	var sent int
	var lastErr error
	for _, dv := range devices {
		var err error
		switch {
		case dv.platform == "ios" && p.APNs != nil:
			err = p.APNs.send(ctx, dv.token, msg)
		case dv.platform == "android" && p.FCM != nil:
			err = p.FCM.send(ctx, dv.token, msg)
		default:
			continue
		}
		if errors.Is(err, ErrDeadToken) {
			_, _ = p.Pool.Exec(ctx,
				`UPDATE sessions SET push_token = NULL WHERE id = $1::uuid`, dv.session)
			continue
		}
		if err != nil {
			lastErr = err
			continue
		}
		sent++
	}
	if sent > 0 {
		return fmt.Sprintf("push:%d", sent), nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("%w: no reachable device", ErrPermanent)
}

func appName(locale string) string {
	if locale == "ar" {
		return "وجبات"
	}
	return "Meals"
}

// FCM sends through Firebase Cloud Messaging's HTTP v1 API.
type FCM struct {
	ProjectID string
	Tokens    oauth2.TokenSource
	Client    *http.Client
	Endpoint  string // tests only
}

// NewFCM builds a sender from a service-account JSON key.
func NewFCM(ctx context.Context, credentialsJSON []byte) (*FCM, error) {
	creds, err := google.CredentialsFromJSON(ctx, credentialsJSON,
		"https://www.googleapis.com/auth/firebase.messaging")
	if err != nil {
		return nil, fmt.Errorf("fcm credentials: %w", err)
	}
	if creds.ProjectID == "" {
		return nil, errors.New("fcm credentials: no project_id")
	}
	return &FCM{ProjectID: creds.ProjectID, Tokens: creds.TokenSource}, nil
}

func (f *FCM) send(ctx context.Context, token string, m pushMessage) error {
	tok, err := f.Tokens.Token()
	if err != nil {
		return fmt.Errorf("fcm auth: %w", err)
	}
	data := map[string]string{}
	if m.Link != "" {
		data["link"] = m.Link
	}
	body, _ := json.Marshal(map[string]any{"message": map[string]any{
		"token":        token,
		"notification": map[string]string{"title": m.Title, "body": m.Body},
		"data":         data,
		"android":      map[string]any{"priority": "high"},
	}})
	endpoint := f.Endpoint
	if endpoint == "" {
		endpoint = "https://fcm.googleapis.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		endpoint+"/v1/projects/"+f.ProjectID+"/messages:send", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := client(f.Client).Do(req)
	if err != nil {
		return fmt.Errorf("fcm: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusOK {
		return nil
	}
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	text := string(raw)
	switch {
	case res.StatusCode == http.StatusNotFound || strings.Contains(text, "UNREGISTERED"):
		return ErrDeadToken
	case res.StatusCode == http.StatusBadRequest && strings.Contains(text, "registration token"):
		return ErrDeadToken
	case res.StatusCode >= 500 || res.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("fcm: status %d", res.StatusCode)
	}
	return fmt.Errorf("%w: fcm status %d", ErrPermanent, res.StatusCode)
}

// APNs sends to Apple with a token-based (.p8) provider key.
type APNs struct {
	Key      *ecdsa.PrivateKey
	KeyID    string
	TeamID   string
	Topic    string // the app's bundle id
	Sandbox  bool
	Client   *http.Client
	Endpoint string // tests only

	mu     sync.Mutex
	jwt    string
	minted time.Time
}

// NewAPNs parses the PEM contents of an Apple .p8 key.
func NewAPNs(p8 []byte, keyID, teamID, topic string, sandbox bool) (*APNs, error) {
	key, err := jwt.ParseECPrivateKeyFromPEM(p8)
	if err != nil {
		return nil, fmt.Errorf("apns key: %w", err)
	}
	return &APNs{Key: key, KeyID: keyID, TeamID: teamID, Topic: topic, Sandbox: sandbox}, nil
}

// bearer is reused for 50 minutes: Apple rejects tokens older than an hour and
// throttles providers that mint a new one per request.
func (a *APNs) bearer() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.jwt != "" && time.Since(a.minted) < 50*time.Minute {
		return a.jwt, nil
	}
	t := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": a.TeamID, "iat": time.Now().Unix(),
	})
	t.Header["kid"] = a.KeyID
	signed, err := t.SignedString(a.Key)
	if err != nil {
		return "", err
	}
	a.jwt, a.minted = signed, time.Now()
	return signed, nil
}

func (a *APNs) send(ctx context.Context, token string, m pushMessage) error {
	bearer, err := a.bearer()
	if err != nil {
		return fmt.Errorf("apns auth: %w", err)
	}
	payload := map[string]any{"aps": map[string]any{
		"alert": map[string]string{"title": m.Title, "body": m.Body},
		"sound": "default",
	}}
	if m.Link != "" {
		payload["link"] = m.Link
	}
	body, _ := json.Marshal(payload)
	endpoint := a.Endpoint
	switch {
	case endpoint != "":
	case a.Sandbox:
		endpoint = "https://api.sandbox.push.apple.com"
	default:
		endpoint = "https://api.push.apple.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		endpoint+"/3/device/"+token, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("authorization", "bearer "+bearer)
	req.Header.Set("apns-topic", a.Topic)
	req.Header.Set("apns-push-type", "alert")
	res, err := client(a.Client).Do(req)
	if err != nil {
		return fmt.Errorf("apns: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusOK {
		return nil
	}
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
	text := string(raw)
	switch {
	case res.StatusCode == http.StatusGone ||
		strings.Contains(text, "BadDeviceToken") || strings.Contains(text, "Unregistered"):
		return ErrDeadToken
	case res.StatusCode >= 500 || res.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("apns: status %d", res.StatusCode)
	}
	return fmt.Errorf("%w: apns status %d %s", ErrPermanent, res.StatusCode, text)
}

func client(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return &http.Client{Timeout: 15 * time.Second}
}
