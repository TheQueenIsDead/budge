package application

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/TheQueenIsDead/budge/pkg/integrations/property"
)

// schedulerTick is how often the scheduler wakes to see whether anything is due.
//
// Jobs are not driven by a ticker each: due-ness is worked out from a persisted
// last-run time, so a restart does not reset a cadence and a machine that was
// asleep does not fire a backlog of missed runs when it wakes.
const schedulerTick = time.Minute

// SourceSchedule names notifications raised by the scheduler itself, as opposed
// to the job it was running.
const SourceSchedule = "Scheduled sync"

// scheduler runs the jobs the owner has switched on. It holds no state beyond
// its own lifetime: what is enabled and when it last ran live in the store, so
// the schedule survives a restart.
type scheduler struct {
	app *Application

	// running serialises the jobs. A manual sync and a scheduled one hitting the
	// same buckets at once is not worth the risk, and a slow run should delay the
	// next rather than overlap it.
	running sync.Mutex

	cancel context.CancelFunc
	done   chan struct{}
}

func newScheduler(app *Application) *scheduler {
	return &scheduler{app: app}
}

// start begins the loop. It returns immediately.
func (s *scheduler) start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})

	go func() {
		defer close(s.done)

		ticker := time.NewTicker(schedulerTick)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runDue(ctx)
			}
		}
	}()
}

// stop ends the loop and waits for a job in flight to finish, so shutdown does
// not cut a sync off midway through writing.
func (s *scheduler) stop() {
	if s.cancel == nil {
		return
	}
	s.cancel()
	<-s.done
}

// runDue runs whichever jobs are due. Each reads the schedule fresh, so toggling
// something off takes effect at the next tick rather than at the next restart.
func (s *scheduler) runDue(ctx context.Context) {
	s.running.Lock()
	defer s.running.Unlock()

	schedule, err := s.app.store.GetScheduleSettings()
	if err != nil {
		s.app.http.Logger.Errorf("scheduler could not read its settings: %v", err)
		return
	}

	now := time.Now()

	if schedule.AkahuEnabled && models.Due(schedule.AkahuLastRun, schedule.AkahuEvery().Every, now) {
		s.runAkahu()
	}

	// Re-read: the Akahu run above has written to the same record.
	if schedule, err = s.app.store.GetScheduleSettings(); err != nil {
		s.app.http.Logger.Errorf("scheduler could not read its settings: %v", err)
		return
	}

	if schedule.AssetsEnabled && models.Due(schedule.AssetsLastRun, schedule.AssetsEvery().Every, now) {
		s.runAssets(ctx)
	}
}

// markRun records that a job has run, whether or not it succeeded. A job that
// fails every time should still back off to its cadence rather than retry on
// every tick.
func (s *scheduler) markRun(job string, at time.Time) {
	schedule, err := s.app.store.GetScheduleSettings()
	if err != nil {
		s.app.http.Logger.Errorf("scheduler could not record a run: %v", err)
		return
	}
	switch job {
	case "akahu":
		schedule.AkahuLastRun = at
	case "assets":
		schedule.AssetsLastRun = at
	}
	if err := s.app.store.SaveScheduleSettings(schedule); err != nil {
		s.app.http.Logger.Errorf("scheduler could not record a run: %v", err)
	}
}

// runAkahu pulls accounts and transactions. Success is silent: a notification
// every hour saying nothing happened would bury the one that matters.
func (s *scheduler) runAkahu() {
	defer s.markRun("akahu", time.Now())

	settings, err := s.app.store.GetAkahuSettings()
	if err != nil {
		s.app.Notify(models.NotificationError, SourceAkahuSync,
			"Scheduled sync could not start", "Akahu settings could not be read.")
		return
	}
	if err := settings.Validate(); err != nil {
		s.app.Notify(models.NotificationError, SourceAkahuSync,
			"Scheduled sync skipped", "Akahu is not configured: "+err.Error())
		return
	}

	if err := s.app.integrations.SyncAkahu(s.app.http.Logger, settings.LastSync); err != nil {
		s.app.Notify(models.NotificationError, SourceAkahuSync,
			"Scheduled sync failed", err.Error())
		return
	}

	if err := s.app.store.UpdateAkahuLastSync(); err != nil {
		s.app.http.Logger.Errorf("could not record the akahu sync time: %v", err)
	}
}

// runAssets refreshes every asset that has a valuation source, and reports the
// ones whose value moved. A property estimate that has not changed is not news.
func (s *scheduler) runAssets(ctx context.Context) {
	defer s.markRun("assets", time.Now())

	assets, err := s.app.store.ReadAssets()
	if err != nil {
		s.app.Notify(models.NotificationError, SourceAssetEstimates,
			"Estimate refresh failed", "Assets could not be read.")
		return
	}

	client := property.New()
	for _, asset := range assets {
		if !asset.Trackable() {
			continue
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
		s.refreshAsset(ctx, client, asset)
	}
}

// refreshAsset fetches one asset's estimate and records it, reporting a move.
func (s *scheduler) refreshAsset(ctx context.Context, client *property.Client, asset models.Asset) {
	// Captured before the write: recording the new figure would otherwise become
	// the thing it is compared against.
	previous := asset.CurrentValue()
	hadValuation := len(asset.Valuations) > 0

	result := client.Estimate(ctx, asset.HomesPropertyID)
	if len(result.Estimates) == 0 {
		s.app.Notify(models.NotificationWarning, SourceAssetEstimates,
			"No estimate for "+asset.Name,
			"No source answered. Nothing was recorded.")
		return
	}

	sources := make([]string, 0, len(result.Estimates))
	for _, estimate := range result.Estimates {
		sources = append(sources, fmt.Sprintf("%s %s", estimate.Source, formatShort(estimate.Value)))
	}

	valuation := models.AssetValuation{
		ID:    newID(),
		Date:  time.Now(),
		Value: result.Average,
		Note:  joinSources(sources),
	}
	if err := s.app.store.AddAssetValuation(asset.Id, valuation); err != nil {
		s.app.Notify(models.NotificationError, SourceAssetEstimates,
			"Could not record an estimate for "+asset.Name, err.Error())
		return
	}

	// The first estimate has nothing to have moved from, so it is recorded
	// quietly rather than announced as a change.
	if !hadValuation || result.Average == previous {
		return
	}

	s.app.Notify(models.NotificationInfo, SourceAssetEstimates,
		asset.Name+" has been revalued",
		describeMove(previous, result.Average, sources))
}

// joinSources reads the sources back as one line.
func joinSources(sources []string) string {
	return strings.Join(sources, " · ")
}

// describeMove says what changed and by how much, in the direction an owner
// thinks about it: up is a gain.
func describeMove(from, to float64, sources []string) string {
	direction := "up"
	if to < from {
		direction = "down"
	}

	message := fmt.Sprintf("%s to %s, %s %s from %s",
		joinSources(sources),
		formatShort(to),
		direction,
		formatShort(math.Abs(to-from)),
		formatShort(from),
	)
	return message
}
