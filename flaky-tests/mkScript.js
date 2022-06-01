const runs = require("./flakyRuns.json")

console.log("#!/usr/bin/env bash")
Object.entries(runs).map(([runID, runAttempt]) => {
	console.log(`RUN_ID=${runID} RUN_ATTEMPT=${runAttempt} ./find-failure.sh;`)
})
