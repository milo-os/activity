package activityprocessor

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

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
