workflow "Build master and deploy on push" {
  on = "push"
  resolves = ["deploy"]
}

action "filter-master-branch" {
  uses = "actions/bin/filter@4227a6636cb419f91a0d1afb1216ecfab99e433a"
  args = "branch master"
}

action "docker-build" {
  uses = "actions/docker/cli@8cdf801b322af5f369e00d85e9cf3a7122f49108"
  needs = [
    "filter-master-branch",
  ]
  args = "build -t naiba/solitudes ."
}

action "docker-login" {
  uses = "actions/docker/login@8cdf801b322af5f369e00d85e9cf3a7122f49108"
  needs = ["docker-build"]
  secrets = ["DOCKER_USERNAME", "DOCKER_PASSWORD"]
}

action "docker-push" {
  uses = "actions/docker/cli@8cdf801b322af5f369e00d85e9cf3a7122f49108"
  needs = ["docker-login"]
  args = "push naiba/solitudes"
}

action "deploy" {
  uses = "maddox/actions/ssh@master"
  needs = ["docker-push"]
  secrets = [
    "PRIVATE_KEY",
    "PUBLIC_KEY",
    "USER",
    "PORT",
    "HOST",
  ]
  args = "/NAIBA/script/solitudes.sh"
}

workflow "Build tag on push" {
  resolves = [
    "docker-push-tag",
  ]
  on = "push"
}

action "filter-tag" {
  uses = "actions/bin/filter@4227a6636cb419f91a0d1afb1216ecfab99e433a"
  args = "ref refs/tags/v*"
}

action "docker-build-tag" {
  uses = "actions/docker/cli@8cdf801b322af5f369e00d85e9cf3a7122f49108"
  needs = [
    "filter-tag",
  ]
  args = "build -t naiba/solitudes ."
}

action "docker-login-tag" {
  uses = "actions/docker/login@8cdf801b322af5f369e00d85e9cf3a7122f49108"
  needs = ["docker-build-tag"]
  secrets = ["DOCKER_USERNAME", "DOCKER_PASSWORD"]
}

action "docker-tag" {
  uses = "actions/docker/tag@8cdf801b322af5f369e00d85e9cf3a7122f49108"
  needs = ["docker-build-tag"]
  args = "naiba/solitudes naiba/solitudes --no-latest --no-sha"
}

action "docker-push-tag" {
  uses = "actions/docker/cli@8cdf801b322af5f369e00d85e9cf3a7122f49108"
  needs = [
    "docker-login-tag",
    "docker-tag",
  ]
  args = "push naiba/solitudes"
}
