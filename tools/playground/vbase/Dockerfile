FROM ubuntu
RUN apt-get update
RUN apt-get install -y gcc make libtool curl git mercurial
RUN mkdir -p /usr/local/veyron/.bin
RUN curl http://commondatastorage.googleapis.com/git-repo-downloads/repo -Lo /usr/local/veyron/.bin/repo
RUN chmod a+x /usr/local/veyron/.bin/repo
WORKDIR /usr/local/veyron
ENV HOME /root
ADD .netrc /root/.netrc
RUN .bin/repo init -u https://veyron.googlesource.com/manifest
RUN .bin/repo sync
RUN rm /root/.netrc



