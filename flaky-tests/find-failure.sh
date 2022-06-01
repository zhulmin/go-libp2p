#!/usr/bin/env bash
set -eou pipefail
# set -x


if [[ -z "${RUN_ID}" ]]; then
    echo "Missing RUN_ID env var"
    exit 1
fi

if [[ -z "${RUN_ATTEMPT}" ]]; then
    echo "Missing RUN_ATTEMPT env var"
    exit 1
fi

echo "Running for $RUN_ID $RUN_ATTEMPT"

if [ $RUN_ATTEMPT == "1" ]
then
	mkdir -p logs/$RUN_ID;
	cd logs/$RUN_ID;
else
	mkdir -p logs/$RUN_ID-$RUN_ATTEMPT;
	cd logs/$RUN_ID-$RUN_ATTEMPT;
fi

if [ ! -f "$RUN_ID.zip" ]; then
	LOG_URL=$(gh api \
	  -H "Accept: application/vnd.github.v3+json" \
	  /repos/libp2p/go-libp2p/actions/runs/$RUN_ID/attempts/$RUN_ATTEMPT | jq -r .logs_url | cut -c 23-)

	echo "Fetching log from $LOG_URL"

	gh api $LOG_URL > $RUN_ID.zip

	unzip $RUN_ID.zip
fi

ag "FAIL:" > failures.txt;

echo "DONE"

