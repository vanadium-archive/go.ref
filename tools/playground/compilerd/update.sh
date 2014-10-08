#!/bin/bash

# Script to rebuild and deploy the docker image and compiler to the playground
# backends.
#
# Usage:
#     gcutil ssh --project google.com:veyron playground-master
#     sudo su - veyron
#     veyron project update
#     bash $VEYRON_ROOT/go/src/veyron.io/veyron/veyron/tools/playground/compilerd/update.sh

set -e
readonly DATE=$(date +%F)
readonly DISK="pg-data-${DATE}"

function unmount() {
  sudo umount /mnt
  gcloud compute --project "google.com:veyron" instances detach-disk --disk="$DISK" $(hostname) --zone us-central1-a
}

function cleanup() {
  if [[ -e /mnt/compilerd ]]; then
    unmount
    # if the disk was still mounted, nothing is using it so go ahead
    # and delete it.
    gcloud compute --project "google.com:veyron" disks delete "${DISK}" --zone "us-central1-a"
  fi
  sudo docker rm "${DISK}" || true
}
trap cleanup EXIT

function main() {
  if [[ ! -e ~/.netrc ]]; then
    echo "Unable to access git, missing ~/.netrc"
    exit 1
  fi

  gcloud compute --project "google.com:veyron" disks create "${DISK}" --size "200" --zone "us-central1-a" --source-snapshot "pg-data-20140702" --type "pd-standard"
  gcloud compute --project "google.com:veyron" instances attach-disk --disk="$DISK" $(hostname) --zone us-central1-a
  sudo mount /dev/sdb1 /mnt

  # Build the docker image.
  cd ${VEYRON_ROOT}/veyron/go/src/veyron.io/veyron/veyron/tools/playground/builder
  cp ~/.netrc ./netrc
  sudo docker build --no-cache -t playground .
  rm -f ./netrc

  # Export the docker image to disk.
  sudo docker save -o /mnt/playground.tar.gz playground

  # Copy the compilerd binary from the docker image to the disk.
  sudo docker run --name=${DISK} playground || true
  sudo docker cp ${DISK}:/usr/local/veyron/veyron/go/bin/compilerd /tmp
  sudo mv /tmp/compilerd /mnt/compilerd
  sudo docker rm ${DISK}

  # Detach the disk so the backends can mount it.
  unmount

  # Update the template to use the new disk.
  cd ../compilerd
  sed -i -e s/pg-data-20140820/${DISK}/ pool_template.json
  gcloud preview replica-pools --zone=us-central1-a update-template --template=pool_template.json playground-pool
  git checkout -- pool_template.json

  # Rolling restart of the replicas.
  INSTANCES=$(gcloud preview replica-pools --zone=us-central1-a replicas --pool=playground-pool list|grep name:|cut -d: -f2)
  for i in ${INSTANCES}; do
    gcloud preview replica-pools --zone=us-central1-a replicas --pool=playground-pool restart ${i}
    sleep 5m
  done
}

main "$@"
