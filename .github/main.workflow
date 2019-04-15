workflow "Build and deploy on push" {
  on = "push"
  resolves = [
    "docker-build-lastest",
    "GitHub Action for Docker",
  ]
}

action "filter-master-branch" {
  uses = "actions/bin/filter@4227a6636cb419f91a0d1afb1216ecfab99e433a"
  args = "branch master"
}

action "docker-build-lastest" {
  uses = "actions/docker/cli@8cdf801b322af5f369e00d85e9cf3a7122f49108"
  needs = [
    "filter-master-branch",
  ]
  args = "build -t naiba/solitudes ."
}

action "filter-tag" {
  uses = "actions/bin/filter@4227a6636cb419f91a0d1afb1216ecfab99e433a"
  args = "tag v*"
  runs = "TAG=$GITHUB_REF"
}

action "GitHub Action for Docker" {
  uses = "actions/docker/cli@8cdf801b322af5f369e00d85e9cf3a7122f49108"
  needs = ["filter-tag"]
  args = "docker build -t naiba/solitudes:$GITHUB_REF"
}
