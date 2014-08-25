# Running the playground compile server locally

Install Docker for Linux:

TODO(sadovsky): These instructions don't seem to work, and furthermore, adding
docker.list to sources.list.d may pose a security risk.

  $ sudo sh -c "echo deb https://get.docker.io/ubuntu docker main > /etc/apt/sources.list.d/docker.list"
  $ sudo apt-get update
  $ sudo apt-get install lxc-docker
  $ aptitude versions lxc-docker

Or for OS X, from this site:

  https://github.com/boot2docker/osx-installer/releases

Build the playground Docker image (this will take a while...):

  $ cp ~/.netrc $VEYRON_ROOT/veyron/go/src/veyron/tools/playground/builder/netrc

  $ sudo docker build -t playground $VEYRON_ROOT/veyron/go/src/veyron/tools/playground/builder/.

Install the playground binaries:

  $ go install veyron/tools/playground/...

Run the compiler binary as root:

  $ sudo go/bin/compilerd --shutdown=false --address=localhost:8181

The server should now be running at http://localhost:8181 and responding to
compile requests at http://localhost:8181/compile.
