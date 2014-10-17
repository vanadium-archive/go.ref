FROM ubuntu
RUN /usr/sbin/useradd -d /home/playground -m playground

# Install various prereqs.
RUN apt-get update
RUN apt-get install -y curl g++ git libc6-i386 make python

# Install Go. Note, the apt-get "golang" target is too old.
RUN (cd /tmp; curl -O https://storage.googleapis.com/golang/go1.3.3.linux-amd64.tar.gz)
RUN tar -C /usr/local -xzf /tmp/go1.3.3.linux-amd64.tar.gz
ENV PATH /usr/local/go/bin:$PATH

ENV HOME /root
ENV VEYRON_ROOT /usr/local/veyron
ENV GOPATH /home/playground:$VEYRON_ROOT/veyron/go
ENV VDLPATH $GOPATH

# Setup veyron and veyron profiles.
# Note: This will be cached! If you want to re-build the docker image using
# fresh veyron code, you must pass "--no-cache" to the docker build command.
# See README.md.
ADD netrc /root/.netrc
RUN curl -u veyron:D6HT]P,LrJ7e https://www.envyor.com/noproxy/veyron-setup.sh | bash
RUN rm /root/.netrc
RUN $VEYRON_ROOT/bin/veyron profile setup web

# Install the veyron.js library.
# TODO(nlacasse): Switch to "npm install -g veyron" once veyron.js is publicly
# visible in NPM.
WORKDIR /usr/local/veyron/veyron.js/
# NOTE(sadovsky): NPM is flaky. If any of the NPM commands below fail, simply
# retry them.
RUN $VEYRON_ROOT/environment/cout/node/bin/npm install --production
RUN $VEYRON_ROOT/environment/cout/node/bin/npm link
WORKDIR /home/playground
RUN $VEYRON_ROOT/environment/cout/node/bin/npm link veyron

# Install Veyron Go dependencies.
WORKDIR /usr/local/veyron/veyron
ENV PATH $VEYRON_ROOT/veyron/go/bin:$VEYRON_ROOT/bin:$PATH
RUN veyron go install veyron.io/veyron/...

# Uncomment the following lines to install a version of the builder tool using
# your local version of the code. This is useful when developing and testing
# local changes to the builder tool.
# RUN rm $VEYRON_ROOT/veyron/go/bin/builder
# ADD main.go /usr/local/veyron/veyron/go/src/veyron/tools/playground/builder/main.go
# ADD identity.go /usr/local/veyron/veyron/go/src/veyron/tools/playground/builder/identity.go
# ADD services.go /usr/local/veyron/veyron/go/src/veyron/tools/playground/builder/services.go
# RUN veyron go install veyron/tools/playground/builder

USER playground
WORKDIR /home/playground
ENTRYPOINT /usr/local/veyron/veyron/go/bin/builder
