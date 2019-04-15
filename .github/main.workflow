workflow "Build master and deploy on push" {
  on = "push"
  resolves = ["docker-push"]
}

action "filter-master-branch" {
  uses = "actions/bin/filter@4227a6636cb419f91a0d1afb1216ecfab99e433a"
  args = "branch master"
}

action "docker-build-master" {
  uses = "actions/docker/cli@8cdf801b322af5f369e00d85e9cf3a7122f49108"
  needs = [
    "filter-master-branch",
  ]
  args = "build -t naiba/solitudes ."
}

action "docker-login-master" {
  uses = "actions/docker/login@8cdf801b322af5f369e00d85e9cf3a7122f49108"
  needs = ["docker-build-master"]
  secrets = ["DOCKER_USERNAME", "DOCKER_PASSWORD"]
}

action "docker-push-master" {
  uses = "actions/docker/cli@8cdf801b322af5f369e00d85e9cf3a7122f49108"
  needs = ["docker-login-master"]
  args = "push naiba/solitudes"
}

workflow "Build tag on push" {
  on = "push"
  resolves = ["docker-push-tag"]
}

action "filter-tag" {
  uses = "actions/bin/filter@4227a6636cb419f91a0d1afb1216ecfab99e433a"
  args = "tag v*"
}

action "docker-build-tag" {
  uses = "actions/docker/cli@8cdf801b322af5f369e00d85e9cf3a7122f49108"
  needs = ["filter-tag"]
  args = "build -t naiba/solitudes:$GITHUB_REF"
}

action "docker-login-tag" {
  uses = "actions/docker/login@8cdf801b322af5f369e00d85e9cf3a7122f49108"
  needs = ["docker-build-tag"]
  secrets = ["DOCKER_USERNAME", "DOCKER_PASSWORD"]
}

action "docker-push-tag" {
  uses = "actions/docker/cli@8cdf801b322af5f369e00d85e9cf3a7122f49108"
  needs = ["docker-login-tag"]
  args = "push naiba/solitudes:$GITHUB_REF"
}

action "docker-login" {
  uses = "actions/docker/login@8cdf801b322af5f369e00d85e9cf3a7122f49108"
  needs = ["docker-build-master"]
  secrets = ["DOCKER_PASSWORD", "DOCKER_USERNAME"]
}

action "docker-push" {
  uses = "actions/docker/cli@8cdf801b322af5f369e00d85e9cf3a7122f49108"
  args = "push naiba/solitudes"
  needs = ["docker-login"]
}
