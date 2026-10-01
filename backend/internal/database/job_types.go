package database

// JobType is a job type the worker can run. The Jobs page type filter used to
// be a hand-maintained list in the template, which is why release_monitor - the
// scheduler's own job type, 39 of them on the playtest instance - could not be
// filtered to at all, and why a newly added type would have been invisible the
// same way. One list, here, is what the filter renders from.
type JobType struct {
	// Value is what is stored in jobs.job_type and what the filter submits.
	Value string
	// Label is what the filter shows a person.
	Label string
}

// JobTypes lists every job type the worker dispatches, in the order the filter
// should offer them. "All Types" is deliberately absent: that option is the
// empty filter, not a job type.
//
// The worker guards this list against drift in
// TestEveryDispatchedJobTypeIsOfferedByTheFilter, so a case added to
// runMonolithicJob without a row here fails the build rather than becoming a
// type an operator cannot filter to.
var JobTypes = []JobType{
	{Value: "acquisition", Label: "Acquisition"},
	{Value: "artist_scan", Label: "Artist scan"},
	{Value: "scan", Label: "Library scan"},
	{Value: "sync", Label: "Sync"},
	{Value: "enrich", Label: "Enrich"},
	{Value: "prune", Label: "Prune"},
	{Value: "release_monitor", Label: "Release monitor"},
	{Value: "index_refresh", Label: "Index refresh"},
}

// JobTypeValues returns the stored value of every known job type.
func JobTypeValues() []string {
	values := make([]string, 0, len(JobTypes))
	for _, jt := range JobTypes {
		values = append(values, jt.Value)
	}
	return values
}

// IsKnownJobType reports whether a stored job type is one the app knows about.
// An unknown value is not an error - rows from a newer build can outlive a
// downgrade - but it is worth logging when one turns up.
func IsKnownJobType(value string) bool {
	for _, jt := range JobTypes {
		if jt.Value == value {
			return true
		}
	}
	return false
}
