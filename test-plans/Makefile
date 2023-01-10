all: ping-versions.json ping-image.tar

ping-image.json: PingDockerfile go.mod go.sum cmd/ping/
	cd .. && docker build --iidfile test-plans/ping-imageID.tmp -f test-plans/PingDockerfile .
	cat ping-imageID.tmp | \
		xargs -I {} echo "{\"imageID\": \"{}\"}" > $@
	rm ping-imageID.tmp

ping-versions.json: ping-image.json
	cat ping-versions.template.json | jq ".[0].containerImageID = $$(jq '.imageID' < ping-image.json)" > $@

ping-image.tar: ping-image.json
	docker image save -o $@ $$(jq -r '.imageID' < ping-image.json)

.PHONY: clean

clean:
	rm ping-image.json