package notify

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/smithy-go"
)

type fakeSNS struct {
	in  *sns.PublishInput
	err error
}

func (f *fakeSNS) Publish(_ context.Context, in *sns.PublishInput, _ ...func(*sns.Options)) (*sns.PublishOutput, error) {
	f.in = in
	if f.err != nil {
		return nil, f.err
	}
	return &sns.PublishOutput{MessageId: aws.String("m1")}, nil
}

func TestSNSSMS(t *testing.T) {
	f := &fakeSNS{}
	ref, err := SNSSMS{API: f, SenderID: "Meals"}.Send(context.Background(),
		Delivery{Destination: "+971500000001", Body: "123456 is your code"})
	if err != nil || ref != "sns:m1" {
		t.Fatalf("ref=%q err=%v", ref, err)
	}
	if aws.ToString(f.in.PhoneNumber) != "+971500000001" ||
		aws.ToString(f.in.MessageAttributes["AWS.SNS.SMS.SMSType"].StringValue) != "Transactional" ||
		aws.ToString(f.in.MessageAttributes["AWS.SNS.SMS.SenderID"].StringValue) != "Meals" {
		t.Fatalf("unexpected input %+v", f.in)
	}

	f.err = &smithy.GenericAPIError{Code: "InvalidParameter"}
	if _, err := (SNSSMS{API: f}).Send(context.Background(), Delivery{}); !errors.Is(err, ErrPermanent) {
		t.Fatalf("invalid number should be permanent, got %v", err)
	}
	f.err = &smithy.GenericAPIError{Code: "Throttling"}
	if _, err := (SNSSMS{API: f}).Send(context.Background(), Delivery{}); err == nil || errors.Is(err, ErrPermanent) {
		t.Fatalf("throttling should be retryable, got %v", err)
	}
}
