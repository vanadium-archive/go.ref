#!/usr/bin/python2.7

# This needs to run on a gce vm with the replica pool
# service account scope (https://www.googleapis.com/auth/ndev.cloudman).
#
# You also need to enable preview in gcloud:
# $ gcloud components update preview
#
# Then add it to your crontab, e.g.
# */10 * * * * gcloud preview replica-pools --zone us-central1-a replicas --pool playground-pool list|monitor.py

import os
import datetime
import subprocess
import sys
import yaml

DESIRED = 2
MAX_ALIVE_MIN = 60
POOL = "playground-pool"

def runCommand(*args):
    cmd = ['gcloud', 'preview', 'replica-pools', '--zone', 'us-central1-a']
    cmd.extend(args)
    subprocess.check_call(cmd)

def resizePool(size):
    runCommand("resize", "--new-size", str(size), POOL)


def shouldRestart(replica):
    if replica['status']['state'] == 'PERMANENTLY_FAILING':
        print "replica %s failed: %s" % (replica['name'], replica['status']['details'])
        return True
    return isTooOld(replica)


def isTooOld(replica):
    start_text = replica['status']['vmStartTime']
    if start_text:
        start = yaml.load(start_text)
        uptime = datetime.datetime.now() - start
        return uptime.seconds > MAX_ALIVE_MIN * 60


def restartReplica(replica):
    print "Restarting replica " + replica['name']
    resizePool(DESIRED + 1)
    runCommand("replicas", "--pool", POOL, "delete", replica['name'])


def maybeRestartReplica(replica):
    if shouldRestart(replica):
        restartReplica(replica)


def main():
    replicas = yaml.load_all(sys.stdin.read())
    for replica in replicas:
        maybeRestartReplica(replica)


if __name__ == "__main__":
    main()
