# Running the playground compile server locally

WARNING: Currently, this will cause your machine to power down after one hour,
so you probably don't want to do this!

TODO(ribrdb): Provide some simple way to run the Docker image locally for
development purposes.

Install Docker:

  $ sudo apt-get install lxc-docker-1.1.0

Build the playground Docker image (this will take a while...):

  $ cp ~/.netrc $VEYRON_ROOT/veyron/go/src/veyron/tools/playground/builder/netrc

  $ sudo docker build -t playground $VEYRON_ROOT/veyron/go/src/veyron/tools/playground/builder/.

Install the playground binaries:

  $ go install veyron/tools/playground/...

Run the compiler binary as root:

  $ sudo compilerd

The server should now be running at http://localhost:8181, and responding to
compile requests at http://localhost:8181/compile.

Note that the server will automatically power down the machine after one hour.
(It will tell you this when it starts.)
