package cel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildEventVars_SourceFieldDefaultsToEmpty is the critical regression
// guard for the "source" CEL variable: it must be present in every activation
// with all-empty-string fields when no source-* annotations exist, never
// omitted. CEL errors on an undefined variable rather than evaluating
// false/empty, so if the builder ever starts omitting "source" conditionally,
// this test catches it - a rule referencing source.city would otherwise error
// on every event today, since no exporter emits these annotations yet.
func TestBuildEventVars_SourceFieldDefaultsToEmpty(t *testing.T) {
	env, err := NewEventEnvironment(nil)
	require.NoError(t, err)

	tests := []struct {
		name       string
		eventMap   map[string]interface{}
		expression string
		want       interface{}
	}{
		{
			name: "event with no metadata at all — source.city equals empty string",
			eventMap: map[string]interface{}{
				"reason": "Scheduled",
			},
			expression: "source.city == ''",
			want:       true,
		},
		{
			name: "event with metadata but no annotations — source.region equals empty string",
			eventMap: map[string]interface{}{
				"reason": "Scheduled",
				"metadata": map[string]interface{}{
					"name": "my-event",
				},
			},
			expression: "source.region == ''",
			want:       true,
		},
		{
			name: "event with annotations but no source-* keys — source.cluster equals empty string",
			eventMap: map[string]interface{}{
				"metadata": map[string]interface{}{
					"annotations": map[string]interface{}{
						"platform.miloapis.com/scope.type": "project",
					},
				},
			},
			expression: "source.cluster == ''",
			want:       true,
		},
		{
			name: "event with source-* annotations — source.city reads the annotation value",
			eventMap: map[string]interface{}{
				"metadata": map[string]interface{}{
					"annotations": map[string]interface{}{
						"activity.miloapis.com/source-plane-type": "edge",
						"activity.miloapis.com/source-cluster":    "cluster-dfw-1",
						"activity.miloapis.com/source-region":     "us-south",
						"activity.miloapis.com/source-city":       "dfw",
					},
				},
			},
			expression: "source.city",
			want:       "dfw",
		},
		{
			name: "event with source-* annotations — source.planeType reads the annotation value",
			eventMap: map[string]interface{}{
				"metadata": map[string]interface{}{
					"annotations": map[string]interface{}{
						"activity.miloapis.com/source-plane-type": "edge",
					},
				},
			},
			expression: "source.planeType",
			want:       "edge",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars := BuildEventVars(tt.eventMap)

			// Guard the exact footgun: "source" must always be a key in the
			// activation map, never conditionally omitted.
			_, ok := vars["source"]
			require.True(t, ok, `"source" must always be present in the event CEL activation`)

			ast, issues := env.Compile(tt.expression)
			require.Nil(t, issues, "compilation issues: %v", issues)

			prg, err := env.Program(ast)
			require.NoError(t, err)

			out, _, err := prg.Eval(vars)
			require.NoError(t, err, "source.* must evaluate without error even when no source annotations are present")

			assert.Equal(t, tt.want, out.Value())
		})
	}
}
