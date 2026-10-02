package activityprocessor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/nats-io/nats.go"

	"go.miloapis.com/activity/internal/processor"
)

func TestClassifyEvaluationError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		ruleIndex int
		want      processor.ErrorType
	}{
		{
			name:      "no rule index is a match error",
			err:       fmt.Errorf("boom"),
			ruleIndex: -1,
			want:      processor.ErrorTypeCELMatch,
		},
		{
			name:      "kind resolution failure",
			err:       fmt.Errorf("wrap: %w", processor.ErrKindResolution),
			ruleIndex: 0,
			want:      processor.ErrorTypeKindResolve,
		},
		{
			name:      "activity build failure resolves to kind",
			err:       fmt.Errorf("wrap: %w", processor.ErrActivityBuild),
			ruleIndex: 2,
			want:      processor.ErrorTypeKindResolve,
		},
		{
			name:      "summary error with rule index",
			err:       fmt.Errorf("rule 0 summary: no such key: name"),
			ruleIndex: 0,
			want:      processor.ErrorTypeCELSummary,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyEvaluationError(tt.err, tt.ruleIndex); got != tt.want {
				t.Errorf("classifyEvaluationError() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReEvaluateDeadLetter_UnmarshalError(t *testing.T) {
	p := &Processor{policyCache: NewPolicyCache()}

	outcome := p.reEvaluateDeadLetter(context.Background(), &processor.DeadLetterEvent{
		Type:            processor.EventTypeAudit,
		OriginalPayload: json.RawMessage(`{not json`),
	})

	if outcome.Resolved {
		t.Fatal("expected unresolved outcome for unparseable payload")
	}
	if outcome.ErrorType != processor.ErrorTypeUnmarshal {
		t.Errorf("ErrorType = %q, want %q", outcome.ErrorType, processor.ErrorTypeUnmarshal)
	}
	if outcome.Err == nil {
		t.Error("expected an error to be returned")
	}
}

func TestReEvaluateDeadLetter_NoPolicyResolves(t *testing.T) {
	p := &Processor{policyCache: NewPolicyCache()}

	payload := []byte(`{"objectRef":{"apiGroup":"example.com","resource":"widgets","name":"w1"}}`)
	outcome := p.reEvaluateDeadLetter(context.Background(), &processor.DeadLetterEvent{
		Type:            processor.EventTypeAudit,
		OriginalPayload: payload,
	})

	if !outcome.Resolved {
		t.Errorf("expected resolved outcome when no policy targets the resource, got err=%v", outcome.Err)
	}
}

// fakeJetStream answers the stream lookup initDLQPublisher makes and records
// what is published on it. The embedded interface is nil, so any method the
// processor does not use panics instead of quietly passing.
type fakeJetStream struct {
	nats.JetStreamContext

	streams    map[string]bool
	streamInfo []string
	published  []string
}

func (f *fakeJetStream) StreamInfo(name string, _ ...nats.JSOpt) (*nats.StreamInfo, error) {
	f.streamInfo = append(f.streamInfo, name)
	if !f.streams[name] {
		return nil, nats.ErrStreamNotFound
	}
	return &nats.StreamInfo{Config: nats.StreamConfig{Name: name}}, nil
}

func (f *fakeJetStream) Publish(subject string, _ []byte, _ ...nats.PubOpt) (*nats.PubAck, error) {
	f.published = append(f.published, subject)
	return &nats.PubAck{Stream: "ACTIVITY_DEAD_LETTER"}, nil
}

// publishDeadLetter drives one failure through the processor's DLQ publisher.
func publishDeadLetter(t *testing.T, p *Processor) {
	t.Helper()
	err := p.dlqPublisher.PublishAuditFailure(context.Background(), json.RawMessage(`{}`),
		"policy", 1, 0, processor.ErrorTypeCELMatch, errors.New("boom"), nil, nil)
	if err != nil {
		t.Fatalf("PublishAuditFailure() = %v, want nil", err)
	}
}

// TestInitDLQPublisher covers which connection the dead-letter stream is
// reached on. It lives on the broker activities are published to, so a
// federated processor must preflight and publish it on the output connection.
func TestInitDLQPublisher(t *testing.T) {
	config := Config{
		DLQEnabled:       true,
		DLQStreamName:    "ACTIVITY_DEAD_LETTER",
		DLQSubjectPrefix: "activity.dlq",
	}
	withStream := func() map[string]bool { return map[string]bool{"ACTIVITY_DEAD_LETTER": true} }

	t.Run("separate output broker: preflight and publish use the output connection", func(t *testing.T) {
		input := &fakeJetStream{}
		output := &fakeJetStream{streams: withStream()}
		p := &Processor{config: config, js: input, outputJS: output}

		if dlq := p.initDLQPublisher(); !dlq.Enabled {
			t.Fatal("DLQ disabled even though the output connection has the stream")
		}
		if len(input.streamInfo) != 0 {
			t.Errorf("preflighted the input connection: %v", input.streamInfo)
		}
		if want := []string{"ACTIVITY_DEAD_LETTER"}; !reflect.DeepEqual(output.streamInfo, want) {
			t.Errorf("output preflight = %v, want %v", output.streamInfo, want)
		}

		publishDeadLetter(t, p)
		if len(input.published) != 0 || len(output.published) != 1 {
			t.Errorf("dead letter landed on input=%v output=%v, want it on the output connection",
				input.published, output.published)
		}
	})

	// Without a second broker outputJS is the input context, so this is the
	// behaviour the management plane already had.
	t.Run("single broker: unchanged, one preflight on the shared connection", func(t *testing.T) {
		js := &fakeJetStream{streams: withStream()}
		p := &Processor{config: config, js: js, outputJS: js}

		if dlq := p.initDLQPublisher(); !dlq.Enabled {
			t.Fatal("DLQ disabled even though the connection has the stream")
		}
		if want := []string{"ACTIVITY_DEAD_LETTER"}; !reflect.DeepEqual(js.streamInfo, want) {
			t.Errorf("preflight = %v, want %v", js.streamInfo, want)
		}

		publishDeadLetter(t, p)
		if len(js.published) != 1 {
			t.Errorf("published = %v, want one dead letter", js.published)
		}
	})

	t.Run("stream absent: DLQ soft-disabled rather than fatal", func(t *testing.T) {
		js := &fakeJetStream{}
		p := &Processor{config: config, js: js, outputJS: js}

		if dlq := p.initDLQPublisher(); dlq.Enabled {
			t.Error("DLQ enabled even though the stream is absent")
		}

		publishDeadLetter(t, p)
		if len(js.published) != 0 {
			t.Errorf("published = %v, want nothing on a disabled DLQ", js.published)
		}
	})
}
