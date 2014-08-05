# mdb example

A simple "movie database" example of store usage.

## How to run

Simply run `make run`. Under the hood, this generates a self-signed identity,
starts a mounttable daemon, starts a store daemon, and initializes the store
with mdb data and templates.

Once everything's up and running, visit the store daemon's viewer in your
browser (http://localhost:5000 by default) to explore the mdb data.
