#!/bin/sh
# psql in the terminal recorder (#1313): the lab's Postgres client, whose
# libraries the Dockerfile keeps beside it, so it is found for psql alone and
# shadows nothing VHS's own browser and encoder load.
exec env LD_LIBRARY_PATH=/usr/local/lib/psql /usr/local/lib/psql/psql "$@"
