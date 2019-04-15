workflow "Build and deploy on push" {
  on = "push"
  resolves = ["docker-build", "Filters for GitHub Actions"]
}

action "filter-master-branch" {
  uses = "actions/bin/filter@master"
  args = "branch master"
}

action "docker-build" {
  uses = "actions/docker/cli@master"
  needs = ["filter-master-branch", "Filters for GitHub Actions"]
  args = "build -t naiba/solitudes:$TAG"
}

action "Filters for GitHub Actions" {
  uses = "actions/bin/filter@master"
  args = "tag v*"
  runs = "TAG=$GITHUB_REF"
}
