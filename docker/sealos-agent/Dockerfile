# Copyright 2026 sealos.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

FROM ubuntu:22.04

RUN apt-get update -y \
  && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    fuse-overlayfs \
    uidmap \
  && rm -rf /var/lib/apt/lists/*

COPY sealos-agent /usr/bin/sealos-agent
RUN chmod 0755 /usr/bin/sealos-agent

ENTRYPOINT ["/usr/bin/sealos-agent"]
CMD ["--help"]
