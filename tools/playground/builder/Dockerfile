FROM vbase
RUN adduser --quiet --disabled-password playground
ENV VEYRON_ROOT /usr/local/veyron

# Setup the development environment
# (based on $VERON_ROOT/environment/scripts/setup/machine/init.sh)
ENV MY_DIR /usr/local/veyron/environment/scripts/setup/machine
ENV PKG_MANAGER apt-get
WORKDIR /usr/local/veyron/environment/scripts/setup/machine

RUN ./go.sh
RUN apt-get install -y autoconf
RUN apt-get build-dep -y libbluetooth-dev

RUN cd $VEYRON_ROOT/environment/csrc/expat-2.1.0 && ./configure --prefix=$VEYRON_ROOT/environment/cout/expat-2.1.0/x86_64 && make -j64 && make install && make distclean
ENV CFLAGS -I/usr/local/veyron/environment/cout/expat-2.1.0/x86_64/include
ENV LDFLAGS -L/usr/local/veyron/environment/cout/expat-2.1.0/x86_64/lib -lexpat
RUN cd $VEYRON_ROOT/environment/csrc/dbus-1.6.14 && autoreconf --install --force --verbose && ./configure --prefix=$VEYRON_ROOT/environment/cout/dbus-1.6.14/x86_64 && make -j64 && make install && make distclean || cat config.log

RUN ./bluez.sh
RUN ./dbus.sh foo
RUN ./expat.sh
RUN ls /usr/local/veyron/environment/cout/
# RUN nacl_sdk.sh
# RUN node.sh

# Build and install the veyron development tool

WORKDIR /usr/local/veyron/veyron
RUN scripts/build/go build -o ../.bin/veyron tools/veyron
RUN scripts/build/go install veyron/... veyron2/...


ENV PATH /usr/local/veyron/veyron/go/bin:/usr/local/veyron/veyron/scripts/build:/usr/local/bin:/usr/bin:/bin
ENV GOPATH /usr/local/veyron/playground
ADD vbuild.go /usr/local/veyron/playground/src/vbuild/main.go
RUN go install vbuild

USER playground
WORKDIR /home/playground
ENV GOPATH /home/playground
ENTRYPOINT /usr/local/veyron/playground/bin/vbuild
