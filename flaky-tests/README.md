# Steps to list top flaky tests

1. Fork this notebook:
https://observablehq.com/@realmarcopolo/integration-test-flakiness
1. Add your Github access token (no permissions required) by following steps in
   the notebook.

That will generate some preliminary data on failures and flakes.

For finding the top flakey tests we need to fetch the logs from the runs
themselves.

1. Install the github cli `gh` we use this to make api requests to github.

1. Find the variable `flakyRunIDs` and download the json to a file. This is the run
id and attempt number.
1. Save that file to flakyRuns.json in the `flaky-tests` directory.
1. Run `node mkScript.js > find-all-failures.sh` this parses the json into a
   script file.
1. `chmod +x find-all-failures.sh`
1. `./find-all-failures.sh`. This will fetch all failures from github with `gh`
   then unzip them in the logs folder, and highlight the failures in the log
   folder under failures.txt.
1. Coalesce all the failures into a single failure txt file with:  `cat
   logs/*/failures.txt > all-failures.txt`

Now time to load this data into our second top flakey tests notebook.

1. Fork https://observablehq.com/@realmarcopolo/top-flaky-tests-on-go-libp2p
1. Attach our all-failures.txt file we generated
1. Update the data variable to use the new data.

done!


