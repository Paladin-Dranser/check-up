module paladin-dranser/check-up

go 1.19

replace check-up/jUnit => ./modules/jUnit

replace check-up/bash => ./modules/bash

require (
	check-up/bash v0.0.0-00010101000000-000000000000
	check-up/jUnit v0.0.0-00010101000000-000000000000
	gopkg.in/yaml.v2 v2.4.0
)
