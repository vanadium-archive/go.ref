# Run allocatord locally

```
# Get oauth credentials file.
gsutil cp gs://vanadium-backup/allocatord_oauth.json /tmp/oauth.json

# Prepare.
source ${JIRI_ROOT}/infrastructure/scripts/util/vanadium-oncall.sh
jiri go install v.io/x/ref/services/allocator/allocatord v.io/x/ref/services/agent/vbecome
agent -s on

# Run allocatord.
# And then visit http://localhost:8166 to browse the UI.
#
# To update UI, edit template/CSS/JavaScript files in the "assets" directory and
# refresh the page to see the changes. No need to restart allocatord.
as_service ${JIRI_ROOT}/release/go/bin/vbecome --name=allocatord \
${JIRI_ROOT}/release/go/bin/allocatord \
 --http-addr=:8166 \
 --external-url=http://localhost:8166 \
 --oauth-client-creds-file=/tmp/oauth.json \
 --secure-cookies=false \
 --deployment-template=${JIRI_ROOT}/infrastructure/gke/syncbase-users-staging/conf/syncbased-deployment.json-template \
 --server-name=syncbased \
 --max-instances-per-user=3 \
 --vkube-cfg=${JIRI_ROOT}/infrastructure/gke/syncbase-users-staging/vkube.cfg \
 --dashboard-gcm-metric=cloud-syncbase \
 --dashboard-gcm-project=vanadium-staging \
 --assets=${JIRI_ROOT}/release/go/src/v.io/x/ref/services/allocator/allocatord/assets \
 --static-assets-prefix=https://static.staging.v.io

# Re-generate "assets/assets.go" file when UI changes are ready to be reviewed.
jiri go generate v.io/x/ref/services/allocator/allocatord
```

