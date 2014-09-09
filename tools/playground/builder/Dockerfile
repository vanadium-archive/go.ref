FROM ubuntu
RUN /usr/sbin/useradd -d /home/playground -m playground
RUN apt-get update
RUN apt-get install -y curl g++ git golang libc6-i386 make python
ENV HOME /root
ENV VEYRON_ROOT /usr/local/veyron
ENV GOPATH /home/playground

# Setup veyron and veyron profiles.
# Note: This will be cached! If you want to re-build the docker image using
# fresh veyron code, you must pass "--no-cache" to the docker build command.
# See README.md.
ADD netrc /root/.netrc
RUN curl -u veyron:D6HT]P,LrJ7e https://www.envyor.com/noproxy/veyron-setup.sh | bash
RUN rm /root/.netrc
RUN /usr/local/veyron/bin/veyron profile setup core
RUN /usr/local/veyron/bin/veyron profile setup web

# Install the veyron.js library.
# TODO(nlacasse): Change to "npm install -g veyron" once it's publicly published
# in NPM.
WORKDIR /usr/local/veyron/veyron.js/
RUN $VEYRON_ROOT/environment/cout/node/bin/npm install --production
RUN $VEYRON_ROOT/environment/cout/node/bin/npm link
WORKDIR /home/playground
RUN $VEYRON_ROOT/environment/cout/node/bin/npm link veyron

# Install Veyron Go dependencies.
WORKDIR /usr/local/veyron/veyron
ENV PATH /usr/local/veyron/veyron/go/bin:/usr/local/bin:/usr/bin:/bin
RUN $VEYRON_ROOT/veyron/scripts/build/go install veyron/... veyron2/...

# Uncomment the following lines to install a version of the builder tool using
# the local copy of vbuild.go.  This is useful when developing and testing
# local changes to the builder tool.
# RUN rm $VEYRON_ROOT/veyron/go/bin/builder
# ADD vbuild.go /usr/local/veyron/veyron/go/src/veyron/tools/playground/builder/vbuild.go
# RUN $VEYRON_ROOT/veyron/scripts/build/go install veyron/tools/playground/builder

USER playground
WORKDIR /home/playground
ENTRYPOINT /usr/local/veyron/veyron/go/bin/builder
