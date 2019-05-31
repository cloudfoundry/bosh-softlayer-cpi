#!/usr/bin/env bash

set -e

source bosh-cpi-release/ci/tasks/utils.sh

check_param S3_ACCESS_KEY_ID
check_param S3_SECRET_ACCESS_KEY

source /etc/profile.d/chruby.sh
chruby ruby

mv bosh-cli/bosh-cli-* /usr/local/bin/bosh-cli
chmod +x /usr/local/bin/bosh-cli

integer_version=$( cat version-semver/number | cut -d. -f1 )
echo $integer_version > promoted/integer_version

cp -r bosh-cpi-release promoted/repo

dev_release=$(echo $PWD/bosh-cpi-dev-artifacts/*.tgz)

pushd promoted/repo
  git pull
  echo creating config/private.yml with blobstore secrets
  cat > config/private.yml << EOF
---
blobstore:
  provider: s3
  options:
    access_key_id: $S3_ACCESS_KEY_ID
    secret_access_key: $S3_SECRET_ACCESS_KEY
EOF

  echo "using bosh CLI version..."
  bosh-cli --version

  echo "finalizing CPI release..."
  bosh-cli finalize-release ${dev_release} --version $integer_version

  rm config/private.yml

  git diff | cat
  git add .

  git config --global user.email "ci@localhost"
  git config --global user.name CI
  git commit -m "New final release v $integer_version"
popd


