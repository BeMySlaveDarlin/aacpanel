# Level 1 stand: a colleague's machine drawn as a container — Ubuntu 24.04
# without a desktop, its own docker inside, systemd as PID 1. Its own docker
# daemon rather than the host socket, so the stand cannot collide with the
# production containers of the panel.
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive
# systemd reads it to see that it runs in a container and stays away from
# hardware.
ENV container=docker

# Only what a colleague really has. golang-go is the forced exception: the
# executor is built on the spot, and apt gives 1.22 while go.mod asks for
# newer — which the stand has to catch. tmux, jq and age are absent on
# purpose: their absence is what the install steps are checked against.
RUN apt-get update && apt-get install -y --no-install-recommends \
        systemd systemd-sysv dbus dbus-user-session libpam-systemd \
        docker.io docker-compose-v2 \
        python3 git ca-certificates curl sudo golang-go \
        iproute2 iputils-ping procps psmisc less nano locales tzdata \
    && rm -rf /var/lib/apt/lists/*

# English locale and only it: a Russian one may be missing on the machine
# the panel is installed on.
RUN sed -i 's/^# *en_US.UTF-8/en_US.UTF-8/' /etc/locale.gen && locale-gen
ENV LANG=en_US.UTF-8

# Units that have nothing to do in a container. tmp.mount would move /tmp
# into RAM, where builds and session scratchpads write.
RUN systemctl mask systemd-udevd.service systemd-udevd-kernel.socket \
        systemd-udevd-control.socket systemd-modules-load.service \
        systemd-journald-audit.socket tmp.mount \
    && systemctl enable docker.service

# The stand user is uid 1001 on purpose: uid 1000 is taken by `ubuntu` from
# the base image, so a hardcoded 1000 in the panel does not look correct.
RUN useradd -m -u 1001 -s /bin/bash -G sudo,docker dev \
    && echo 'dev ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/90-dev \
    && chmod 0440 /etc/sudoers.d/90-dev

# What systemd is asked to shut down with; without it `docker stop` kills
# init with a signal it does not expect.
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
