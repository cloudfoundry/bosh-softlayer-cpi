#!/bin/bash

set -e -x

: "${BAT_VCAP_PASSWORD:?}"
: "${SL_DATACENTER:?}"
: "${SL_VLAN_PRIVATE:?}"
: "${SL_VLAN_PUBLIC:?}"
: "${SL_VM_DOMAIN:?}"
: "${SL_VM_NAME_PREFIX:?}"
: "${STEMCELL_NAME:?}"

state_path() { bosh-cli int director-state/director.yml --path="$1" ; }
creds_path() { bosh-cli int director-state/director-creds.yml --path="$1" ; }

cat > bats-config/bats.env <<EOF
export BOSH_ENVIRONMENT="$( jq -r ".current_ip" director-state/director-state.json 2>/dev/null )"
export BOSH_CLIENT="admin"
export BOSH_CLIENT_SECRET="$( creds_path /admin_password )"
export BOSH_CA_CERT="$( creds_path /director_ssl/ca )"
export BOSH_GW_HOST="$( jq -r ".current_ip" director-state/director-state.json 2>/dev/null )"
export BOSH_GW_USER="jumpbox"
export BOSH_OS_BATS=false

export BAT_PRIVATE_KEY="$( creds_path /jumpbox_ssh/private_key )"
export BAT_DNS_HOST="$( jq -r ".current_ip" director-state/director-state.json 2>/dev/null )"
export BAT_INFRASTRUCTURE=softlayer
export BAT_NETWORKING=dynamic
export BAT_RSPEC_FLAGS="--tag ~vip_networking --tag ~manual_networking --tag ~root_partition --tag ~raw_ephemeral_storage --tag ~multiple_manual_networks"
export BAT_DIRECTOR=$( jq -r ".current_ip" director-state/director-state.json 2>/dev/null )
export BAT_DEBUG_MODE=true
EOF

cat > interpolate.yml <<EOF
---
cpi: softlayer
properties:
  use_static_ip: false
  use_vip: false
  pool_size: 1
  instances: 1
  stemcell:
    name: ((STEMCELL_NAME))
    version: latest
  cloud_properties:
    hostname_prefix: ((SL_VM_NAME_PREFIX))
    datacenter: ((SL_DATACENTER))
    domain: ((SL_VM_DOMAIN))
  networks:
  - name: default
    type: dynamic
    cloud_properties:
      vlan_ids:
      - ((SL_VLAN_PUBLIC))
      - ((SL_VLAN_PRIVATE))
  password: "\$1\$LxAOw3r5\$XUpSO1fAIsT5SOl8bEeHF0"
  dns:
  - 8.8.8.8
  - 10.0.80.11
  - 10.0.80.12
EOF

echo -e "\\n\\033[32m[INFO] Interpolating deployment manifest of bats.\\033[0m"
bosh-cli interpolate \
 -v STEMCELL_NAME="${STEMCELL_NAME}" \
 -v SL_VM_NAME_PREFIX="${SL_VM_NAME_PREFIX}" \
 -v SL_DATACENTER="${SL_DATACENTER}" \
 -v SL_VM_DOMAIN="${SL_VM_DOMAIN}" \
 -v SL_VLAN_PUBLIC="${SL_VLAN_PUBLIC}" \
 -v SL_VLAN_PRIVATE="${SL_VLAN_PRIVATE}" \
 interpolate.yml \
 > bats-config/bats-config.yml
