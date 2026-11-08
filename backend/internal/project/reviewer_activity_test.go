package project

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReviewerActivityData(t *testing.T) {
	// Cada tipo copia só os campos permitidos; o resto do payload (IP, justificativa, nome) nunca passa.
	payload := []byte(`{"reason":"denied","note":"texto interno","justification":"segredo","ip":"10.0.0.1","round":9}`)
	assert.Equal(t, map[string]any{"reason": "denied"}, reviewerActivityData(QueueActArchived, payload, 0))
	assert.Equal(t, map[string]any{"reason": ArchiveOther}, reviewerActivityData(QueueActArchived, []byte(`{"reason":"inventado"}`), 0), "motivo fora da lista vira 'other'")
	for _, kind := range []string{ReviewerActIndicated, QueueActFramingConfirmed, QueueActFramingRefused} {
		assert.Equal(t, map[string]any{"round": 2}, reviewerActivityData(kind, payload, 2), kind)
	}
	assert.Equal(t, map[string]any{}, reviewerActivityData("outro", payload, 3), "tipo fora da lista não leva nada")
}
