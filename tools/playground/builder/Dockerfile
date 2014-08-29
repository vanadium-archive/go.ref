FROM ubuntu
RUN apt-get update
RUN apt-get install -y curl git golang
ENV HOME /root
ENV VEYRON_ROOT /usr/local/veyron
ADD netrc /root/.netrc
RUN curl -u veyron:D6HT]P,LrJ7e https://www.envyor.com/noproxy/veyron-setup.sh | bash
RUN /usr/local/veyron/bin/veyron profile setup core
RUN rm /root/.netrc

# Uncomment the following line to use the local copy of vbuild.go.  This is
# useful when developing and testing local changes to vbuild.go.
# ADD vbuild.go $VEYRON_ROOT/veyron/go/src/tools/playground/builder

WORKDIR /usr/local/veyron/veyron
ENV PATH /usr/local/veyron/veyron/go/bin:/usr/local/bin:/usr/bin:/bin
RUN $VEYRON_ROOT/veyron/scripts/build/go install veyron/tools/identity veyron/services/mounttable/mounttabled veyron2/vdl/vdl
RUN $VEYRON_ROOT/veyron/scripts/build/go install veyron/tools/playground/builder veyron/tools/playground/compilerd

RUN /usr/sbin/useradd -d /home/playground -m playground

USER playground
WORKDIR /home/playground
ENV GOPATH /home/playground
ENTRYPOINT /usr/local/veyron/veyron/go/bin/builder
