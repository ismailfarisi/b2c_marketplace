package notify

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// PreferenceState is one switch on the account screen.
type PreferenceState struct {
	Category Category `json:"category"`
	Channel  Channel  `json:"channel"`
	Allowed  bool     `json:"allowed"`
	Optional bool     `json:"optional"`
}

// Preferences reads back what SetPreference stored, as one row per channel
// marketing can travel on, so a client can draw the switches in their current
// position. Channels with no stored row report the default (allowed).
func (s *Service) Preferences(ctx context.Context, userID uuid.UUID) ([]PreferenceState, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT category, channel, allowed FROM notification_preferences WHERE user_id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("notify: load preferences: %w", err)
	}
	defer rows.Close()
	var stored []Preference
	for rows.Next() {
		var p Preference
		if err := rows.Scan(&p.Category, &p.Channel, &p.Allowed); err != nil {
			return nil, fmt.Errorf("notify: scan preference: %w", err)
		}
		stored = append(stored, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return preferenceStates(stored), nil
}

func preferenceStates(stored []Preference) []PreferenceState {
	out := []PreferenceState{}
	for _, ch := range ChannelsFor(CategoryMarketing) {
		out = append(out, PreferenceState{
			Category: CategoryMarketing, Channel: ch,
			Allowed: Allowed(CategoryMarketing, ch, stored), Optional: true,
		})
	}
	return out
}
