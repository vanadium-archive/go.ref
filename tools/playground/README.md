# Running the playground compile server locally

Install Docker on Goobuntu:

  * http://go/installdocker

Install Docker on OS X:

  * https://github.com/boot2docker/osx-installer/releases

Build the playground Docker image (this will take a while...):

  $ cp ~/.netrc $VEYRON_ROOT/veyron/go/src/veyron/tools/playground/builder/netrc

  $ sudo docker build -t playground $VEYRON_ROOT/veyron/go/src/veyron/tools/playground/builder/.

Note: If you want to ensure an up-to-date version of veyron is installed in the
Docker image, run the above command with the "--no-cache" flag.

Install the playground binaries:

  $ go install veyron/tools/playground/...

Run the compiler binary as root:

  $ sudo go/bin/compilerd --shutdown=false --address=localhost:8181

The server should now be running at http://localhost:8181 and responding to
compile requests at http://localhost:8181/compile.
