package engine

import (
	"testing"

	"github.com/A1kartikey/leash/internal/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func hashOf(s string) [32]byte { return crypto.Keccak256Hash([]byte(s)) }

func TestJudge(t *testing.T) {
	const good = `{"temp":21}`
	jsonHash := hashOf(good)

	tests := []struct {
		name        string
		challenge   types.Challenge
		status      int
		contentType string
		body        string
		want        types.VerdictOutcome
		reason      string
	}{
		{"exact hash match", types.Challenge{ResourceHash: jsonHash}, 200, "application/json", good, types.VerdictDelivered, ReasonHashMatch},
		{"hash match on 201", types.Challenge{ResourceHash: jsonHash}, 201, "application/json", good, types.VerdictDelivered, ReasonHashMatch},
		{"non-2xx", types.Challenge{ResourceHash: jsonHash}, 500, "application/json", good, types.VerdictAbsent, ReasonBadStatus},
		{"3xx is not 2xx", types.Challenge{ResourceHash: jsonHash}, 302, "application/json", good, types.VerdictAbsent, ReasonBadStatus},
		{"empty body", types.Challenge{ResourceHash: jsonHash}, 200, "application/json", "", types.VerdictAbsent, ReasonEmptyBody},
		{"empty body beats hash of empty", types.Challenge{ResourceHash: hashOf("")}, 200, "application/json", "", types.VerdictAbsent, ReasonEmptyBody},

		{
			"hash mismatch but contract holds is partial",
			types.Challenge{ResourceHash: jsonHash, ContentType: "application/json", MinBytes: 4},
			200, "application/json", `{"te`, types.VerdictPartial, ReasonHashMismatch,
		},
		{
			"hash mismatch and body too short is absent",
			types.Challenge{ResourceHash: jsonHash, ContentType: "application/json", MinBytes: 8},
			200, "application/json", `{"te`, types.VerdictAbsent, ReasonHashMismatch,
		},
		{
			"hash mismatch and wrong content type is absent",
			types.Challenge{ResourceHash: jsonHash, ContentType: "application/json", MinBytes: 1},
			200, "text/html", "<h1>nope</h1>", types.VerdictAbsent, ReasonHashMismatch,
		},
		{
			"hash mismatch with no contract declared is absent",
			types.Challenge{ResourceHash: jsonHash},
			200, "application/json", "surprise", types.VerdictAbsent, ReasonHashMismatch,
		},

		{
			"no hash, contract satisfied",
			types.Challenge{ContentType: "application/json", MinBytes: 4},
			200, "application/json; charset=utf-8", good, types.VerdictDelivered, ReasonContentContract,
		},
		{
			"no hash, too few bytes",
			types.Challenge{ContentType: "application/json", MinBytes: 100},
			200, "application/json", good, types.VerdictAbsent, ReasonContractViolated,
		},
		{
			"no hash, wrong media type",
			types.Challenge{ContentType: "application/json"},
			200, "text/plain", good, types.VerdictAbsent, ReasonContractViolated,
		},
		{
			"no hash, no contract: any non-empty 2xx delivers",
			types.Challenge{},
			200, "text/plain", "x", types.VerdictDelivered, ReasonContentContract,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Judge(tc.challenge, tc.status, tc.contentType, []byte(tc.body))
			if got.Outcome != tc.want || got.Reason != tc.reason {
				t.Fatalf("got %s/%s, want %s/%s", got.Outcome, got.Reason, tc.want, tc.reason)
			}
		})
	}
}

// Judge must be a pure function: same inputs, same verdict, every time.
func TestJudgeIsDeterministic(t *testing.T) {
	c := types.Challenge{ResourceHash: hashOf("body"), ContentType: "application/json", MinBytes: 2}
	first := Judge(c, 200, "application/json", []byte("body"))
	for i := 0; i < 100; i++ {
		if got := Judge(c, 200, "application/json", []byte("body")); got != first {
			t.Fatalf("iteration %d diverged: %+v vs %+v", i, got, first)
		}
	}
}
