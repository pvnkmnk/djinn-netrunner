package database

// A Schedule runs one job type, and the job type names the scope it runs
// against. The mapping is declared here, once, because three callers have to
// agree on it: the API validates a submitted form against it, the worker
// refuses to queue a job type that is not in it, and the schedule card renders
// its label. A row that arrived from a hand-edited database therefore cannot
// make the scheduler queue work the app does not offer, and no schedule can
// name a watchlist while queueing a library job.
const (
	ScheduleTargetWatchlist = "watchlist"
	ScheduleTargetLibrary   = "library"
)

// ScheduleJobTypes maps a schedulable job type to the kind of scope it runs
// against. Adding a row here is what makes a job type schedulable; the form
// offers it, the API accepts it, the worker dispatches it, and the card labels
// it, with nothing else to keep in step.
var ScheduleJobTypes = map[string]string{
	"sync":   ScheduleTargetWatchlist,
	"scan":   ScheduleTargetLibrary,
	"enrich": ScheduleTargetLibrary,
}

// SchedulableJobTypes returns the schedulable job types in the order a form
// should offer them: the watchlist job first, then the library ones.
func SchedulableJobTypes() []string {
	return []string{"sync", "scan", "enrich"}
}

// SchedulableJobTypeOptions returns the schedulable job types with the labels
// the jobs filter uses, so a schedule is named the same way wherever it is
// shown.
func SchedulableJobTypeOptions() []JobType {
	options := make([]JobType, 0, len(ScheduleJobTypes))
	for _, value := range SchedulableJobTypes() {
		options = append(options, JobType{Value: value, Label: JobTypeLabel(value)})
	}
	return options
}

// ScheduleTargetForJobType returns the scope kind a job type runs against.
func ScheduleTargetForJobType(jobType string) (string, bool) {
	target, ok := ScheduleJobTypes[jobType]
	return target, ok
}

// Scope returns the job scope a schedule queues against, in the shape the
// worker's dispatcher reads (ScopeType, ScopeID). ok is false when the job type
// is not schedulable or the row names no scope -- a column that was never
// written -- which the caller has to treat as a broken schedule rather than
// queue a job whose scope id is empty: every empty-scope job contends on one
// advisory lock and starves the queue.
func (s Schedule) Scope() (scopeType, scopeID string, ok bool) {
	target, known := ScheduleTargetForJobType(s.JobType)
	if !known {
		return "", "", false
	}
	if target == ScheduleTargetWatchlist {
		if s.WatchlistID == nil {
			return ScheduleTargetWatchlist, "", false
		}
		return ScheduleTargetWatchlist, s.WatchlistID.String(), true
	}
	if s.LibraryID == nil {
		return ScheduleTargetLibrary, "", false
	}
	return ScheduleTargetLibrary, s.LibraryID.String(), true
}

// OwnerUserID is the owner of the scope, so a scheduled job is attributed to
// the account that owns the watchlist or library it runs against. The
// association has to be preloaded; nil means "no owner", which both editions
// treat as admin-owned.
func (s Schedule) OwnerUserID() *uint64 {
	switch target, _ := ScheduleTargetForJobType(s.JobType); target {
	case ScheduleTargetWatchlist:
		return s.Watchlist.OwnerUserID
	case ScheduleTargetLibrary:
		return s.Library.OwnerUserID
	}
	return nil
}

// JobLabel is the job type as a person reads it, from the one list the jobs
// filter renders from as well.
func (s Schedule) JobLabel() string {
	return JobTypeLabel(s.JobType)
}

// TargetLabel is the scope this schedule runs against, named Go-side because a
// nil pointer reads as true to pongo2: without it, a schedule whose watchlist
// was deleted renders as an empty name. Like NextRunLabel, it is a method so a
// template can print it.
func (s Schedule) TargetLabel() string {
	switch target, _ := ScheduleTargetForJobType(s.JobType); target {
	case ScheduleTargetWatchlist:
		if s.Watchlist.Name != "" {
			return s.Watchlist.Name
		}
		if s.WatchlistID != nil {
			return "watchlist " + s.WatchlistID.String()
		}
		return "unknown watchlist"
	case ScheduleTargetLibrary:
		if s.Library.Name != "" {
			return s.Library.Name
		}
		if s.LibraryID != nil {
			return "library " + s.LibraryID.String()
		}
		return "unknown library"
	}
	return "unknown job type"
}
