package domain

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

// Short words keep "rs-<adjective>-<noun>" inside Massdriver's 20-character
// organization identifier limit.
var (
	orgAdjectives = []string{
		"amber", "bold", "brisk", "calm", "clear", "cool", "crisp", "dusky", "eager", "fleet",
		"gold", "hazy", "jade", "keen", "lunar", "misty", "noble", "pale", "quiet", "rapid",
		"rosy", "silver", "solar", "steady", "still", "swift", "tidal", "vivid", "warm", "wild",
	}
	orgNouns = []string{
		"brook", "canyon", "cliff", "comet", "delta", "dune", "ember", "falls", "fjord", "glade",
		"harbor", "isle", "lagoon", "meadow", "mesa", "orbit", "peak", "pine", "reef", "ridge",
		"river", "shoal", "spark", "summit", "tide", "trail", "vale", "wave", "willow", "zephyr",
	}
)

// NewOrganizationID returns an identifier like "rs-misty-river-42".
func NewOrganizationID() string {
	adj := orgAdjectives[randomIndex(len(orgAdjectives))]
	noun := orgNouns[randomIndex(len(orgNouns))]
	n := randomIndex(90) + 10
	id := fmt.Sprintf("rs-%s-%s-%d", adj, noun, n)
	if len(id) > 20 {
		id = fmt.Sprintf("rs-%s-%d", noun, n)
	}
	return strings.ToLower(id)
}

func randomIndex(n int) int {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return int(v.Int64())
}
