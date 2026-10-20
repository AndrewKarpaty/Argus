package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/AndrewKarpaty/Argus/internal/domain"
)

// Collector fans telemetry queries out across the configured providers. Any
// provider may be nil, which simply means that class of evidence is not
// available in this deployment.
type Collector struct {
	Metrics MetricsProvider
	Logs    LogsProvider
	Traces  TracesProvider
	Changes ChangeProvider
	Log     *slog.Logger
	// Limit is the per-provider, per-service item cap.
	Limit int
}

// Available lists the wired-up provider classes, for /readyz and for telling
// the model what it can actually ask about.
func (c *Collector) Available() []string {
	var out []string
	if c.Metrics != nil {
		out = append(out, "metrics")
	}
	if c.Logs != nil {
		out = append(out, "logs")
	}
	if c.Traces != nil {
		out = append(out, "traces")
	}
	if c.Changes != nil {
		out = append(out, "changes")
	}
	return out
}

// Gather collects every kind of evidence for every service, concurrently.
// Provider failures are recorded in the bundle instead of aborting the run:
// an investigation with partial data beats no investigation.
func (c *Collector) Gather(ctx context.Context, serviceIDs []string, window domain.TimeWindow) Bundle {
	bundle := Bundle{Window: window}
	if len(serviceIDs) == 0 {
		return bundle
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	fail := func(kind, serviceID string, err error) {
		mu.Lock()
		defer mu.Unlock()
		msg := fmt.Sprintf("%s/%s: %v", kind, serviceID, err)
		bundle.Errors = append(bundle.Errors, msg)
		if c.Log != nil {
			c.Log.Warn("telemetry provider failed", "provider", kind, "service", serviceID, "error", err)
		}
	}

	for _, id := range serviceIDs {
		q := Query{ServiceID: id, Window: window, Limit: c.Limit}

		if c.Metrics != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				signals, err := c.Metrics.Anomalies(ctx, q)
				if err != nil {
					fail("metrics", q.ServiceID, err)
					return
				}
				mu.Lock()
				bundle.Signals = append(bundle.Signals, signals...)
				mu.Unlock()
			}()
		}
		if c.Logs != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				signals, err := c.Logs.ErrorPatterns(ctx, q)
				if err != nil {
					fail("logs", q.ServiceID, err)
					return
				}
				mu.Lock()
				bundle.Signals = append(bundle.Signals, signals...)
				mu.Unlock()
			}()
		}
		if c.Traces != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				paths, err := c.Traces.FailingPaths(ctx, q)
				if err != nil {
					fail("traces", q.ServiceID, err)
					return
				}
				mu.Lock()
				bundle.Paths = append(bundle.Paths, paths...)
				mu.Unlock()
			}()
		}
		if c.Changes != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				changes, err := c.Changes.RecentChanges(ctx, q)
				if err != nil {
					fail("changes", q.ServiceID, err)
					return
				}
				mu.Lock()
				bundle.Changes = append(bundle.Changes, changes...)
				mu.Unlock()
			}()
		}
	}
	wg.Wait()

	// Stable ordering keeps prompts and API responses reproducible.
	sort.SliceStable(bundle.Signals, func(i, j int) bool {
		if !bundle.Signals[i].ObservedAt.Equal(bundle.Signals[j].ObservedAt) {
			return bundle.Signals[i].ObservedAt.Before(bundle.Signals[j].ObservedAt)
		}
		return bundle.Signals[i].ID < bundle.Signals[j].ID
	})
	sort.SliceStable(bundle.Changes, func(i, j int) bool {
		if !bundle.Changes[i].At.Equal(bundle.Changes[j].At) {
			return bundle.Changes[i].At.Before(bundle.Changes[j].At)
		}
		return bundle.Changes[i].ID < bundle.Changes[j].ID
	})
	sort.Strings(bundle.Errors)
	return bundle
}
