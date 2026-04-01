package server

// MaxSyncsPerDay is the maximum number of SimpleFIN syncs allowed in a rolling
// 24-hour window. This stays within SimpleFIN's 24-call daily API cap with
// headroom of 4 calls for debugging and manual queries.
const MaxSyncsPerDay = 20
