FROM ubuntu
RUN apt-get update
RUN apt-get install -y  curl git
ENV HOME /root
ENV VEYRON_ROOT /usr/local/veyron
ADD netrc /root/.netrc
RUN curl -u veyron:D6HT]P,LrJ7e https://www.envyor.com/noproxy/veyron-setup.sh | bash
RUN /veyron/bin/veyron setup proximity
RUN rm /root/.netrc

WORKDIR /usr/local/veyron/veyron
ENV PATH /usr/local/veyron/veyron/go/bin:/usr/local/veyron/veyron/scripts/build:/usr/local/bin:/usr/bin:/bin
RUN go install veyron2/vdl/vdl veyron/tools/playground/builder

RUN adduser --quiet --disabled-password playground

USER playground
WORKDIR /home/playground
ENV GOPATH /home/playground
ENTRYPOINT /usr/local/veyron/veyron/go/bin/builder
