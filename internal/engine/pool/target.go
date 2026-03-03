package pool

import (
	"math/rand"

	"golang.org/x/time/rate"
)

// TargetMeta holds the protocol-agnostic metadata the pool needs for each target.
// The index position in the slice corresponds to the targetIndex passed to WorkerFunc.
type TargetMeta struct {
	RPS     int
	Limiter *rate.Limiter
}

// selectTarget selects a target index weighted by RPS.
func (p *Pool) selectTarget() int {
	r := rand.Intn(p.totalRPS)
	cumulative := 0
	for i, t := range p.targets {
		cumulative += t.RPS
		if r < cumulative {
			return i
		}
	}
	return len(p.targets) - 1
}
