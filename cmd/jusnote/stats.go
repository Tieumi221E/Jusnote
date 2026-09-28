package main

import "github.com/Tieumi221E/Jus/procstat"

// reportStats: with JUSNOTE_STATS set, the peak memory on stderr as the
// process ends (tools/bench.ps1).
func reportStats() { procstat.Report("jusnote", "JUSNOTE_STATS") }
