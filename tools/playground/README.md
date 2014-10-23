# Building a Docker image and running the playground server locally

## Docker setup

Install Docker:

* Goobuntu: http://go/installdocker
* OS X: https://github.com/boot2docker/osx-installer/releases

On Goobuntu, we recommend overriding the default graph dir (`/var/lib/docker`)
to avoid filling up the root filesystem partition, which is quite small. To do
so, add the following line to your `/etc/default/docker`:

    DOCKER_OPTS="${DOCKER_OPTS} -g /usr/local/google/docker"

Start (or restart) the Docker daemon:

    $ sudo service docker restart

Build the playground Docker image (this will take a while...):

    $ cp ~/.netrc $VEYRON_ROOT/veyron/go/src/veyron.io/veyron/veyron/tools/playground/builder/netrc
    $ sudo docker build -t playground $VEYRON_ROOT/veyron/go/src/veyron.io/veyron/veyron/tools/playground/builder/.

Note: If you want to ensure an up-to-date version of veyron is installed in the
Docker image, run the above command with the "--no-cache" flag.

Test your image (without running compilerd):

    $ sudo docker run -i playground < /usr/local/google/home/sadovsky/dev/veyron-www/content/playgrounds/code/fortune/ex0-go/bundle.json

## Running the playground server (compilerd)

Install the playground binaries:

    $ veyron go install veyron.io/veyron/veyron/tools/playground/...

Run the compiler binary as root:

    $ sudo $VEYRON_ROOT/veyron/go/bin/compilerd --shutdown=false --address=localhost:8181

The server should now be running at http://localhost:8181 and responding to
compile requests at http://localhost:8181/compile. Add `?pgaddr=localhost:8181`
to any veyron-www page to make its embedded playgrounds talk to your server.
