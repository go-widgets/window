# The live proof of Config.GPU on Linux, off a GPU: lavapipe (Mesa's software
# Vulkan) draws, Xvfb (X11) and headless sway (Wayland) present, and the tests
# start the servers themselves (WINDOW_GPU_SPAWN=1), so the whole recipe is
# this image plus one command. Build and run (the test binary is cross-built on
# the host, CGO-free, as it would be for a machine with a GPU):
#
#   docker build -t window-gpu-live test/gpu-linux
#   CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c -tags=integration -o /path/work/window.test .
#   docker run --rm -v /path/work:/work -w /work \
#     -e WINDOW_GPU_INTEGRATION=1 -e WINDOW_GPU_REQUIRE=1 -e WINDOW_GPU_SPAWN=1 \
#     window-gpu-live ./window.test -test.run TestLiveGPU -test.v
#
# A non-root user: sway will not run as root.
FROM debian:trixie-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      mesa-vulkan-drivers libvulkan1 vulkan-validationlayers vulkan-tools \
      xvfb x11-apps xdotool imagemagick sway grim libxkbcommon-tools \
      ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home dev
USER dev
