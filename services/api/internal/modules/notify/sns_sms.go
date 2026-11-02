package notify

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/aws/smithy-go"
)

// SNSPublisher is the slice of the SNS client this provider uses.
type SNSPublisher interface {
	Publish(ctx context.Context, in *sns.PublishInput, opts ...func(*sns.Options)) (*sns.PublishOutput, error)
}

// SNSSMS sends SMS through Amazon SNS as transactional messages, which SNS
// routes for delivery rather than cost. UAE delivery needs a registered
// sender ID; without one, carriers there drop or rewrite the sender.
type SNSSMS struct {
	API      SNSPublisher
	SenderID string
}

func (SNSSMS) Name() string { return "sns" }

func (p SNSSMS) Send(ctx context.Context, d Delivery) (string, error) {
	attrs := map[string]types.MessageAttributeValue{
		"AWS.SNS.SMS.SMSType": {DataType: aws.String("String"), StringValue: aws.String("Transactional")},
	}
	if p.SenderID != "" {
		attrs["AWS.SNS.SMS.SenderID"] = types.MessageAttributeValue{
			DataType: aws.String("String"), StringValue: aws.String(p.SenderID),
		}
	}
	out, err := p.API.Publish(ctx, &sns.PublishInput{
		PhoneNumber:       aws.String(d.Destination),
		Message:           aws.String(d.Body),
		MessageAttributes: attrs,
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			switch apiErr.ErrorCode() {
			case "InvalidParameter", "InvalidParameterValue", "OptedOut", "AuthorizationError":
				return "", fmt.Errorf("%w: sns %s", ErrPermanent, apiErr.ErrorCode())
			}
		}
		return "", fmt.Errorf("sns publish: %w", err)
	}
	return "sns:" + aws.ToString(out.MessageId), nil
}
