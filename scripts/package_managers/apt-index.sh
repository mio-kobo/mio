#!/usr/bin/env bash
set -euo pipefail
# Run in a persistent repository snapshot, retaining old pool files for upgrades.
repo=$(cd "$1" && pwd)
: "${MIO_APT_SIGNING_FINGERPRINT:?full dedicated signing fingerprint required}"
[[ "$MIO_APT_SIGNING_FINGERPRINT" =~ ^[A-F0-9]{40}$ ]]
cd "$repo"
mkdir -p dists/stable/main/binary-amd64
apt-ftparchive packages pool > dists/stable/main/binary-amd64/Packages
gzip -n -9 -c dists/stable/main/binary-amd64/Packages > dists/stable/main/binary-amd64/Packages.gz
apt-ftparchive -o APT::FTPArchive::Release::Origin=MioKobo -o APT::FTPArchive::Release::Label=Mio -o APT::FTPArchive::Release::Suite=stable -o APT::FTPArchive::Release::Codename=stable -o APT::FTPArchive::Release::Architectures=amd64 -o APT::FTPArchive::Release::Components=main release dists/stable > dists/stable/Release.new
mv dists/stable/Release.new dists/stable/Release
gpg --batch --yes --local-user "$MIO_APT_SIGNING_FINGERPRINT" --clearsign --output dists/stable/InRelease dists/stable/Release
gpg --batch --yes --local-user "$MIO_APT_SIGNING_FINGERPRINT" --armor --detach-sign --output dists/stable/Release.gpg dists/stable/Release
gpg --batch --export "$MIO_APT_SIGNING_FINGERPRINT" > mio-archive-keyring.gpg
gpgv --keyring "$repo/mio-archive-keyring.gpg" dists/stable/InRelease
