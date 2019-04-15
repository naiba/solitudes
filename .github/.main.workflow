workflow "Build and deploy" {
    on = "push"
}

action "master-branch-filter" {
    uses = "actions/bin/filter@master"
    args = "branch master"
}

action "tag-filter" {
    uses = "actions/bin/filter@master"
    args = "tag v*"
}

action "docker-login" {
    uses = "actions/docker/login@master"
    secrets = [ "DOCKER_USERNAME", "DOCKER_PASSWORD" ]
}

action "build-master" {
    needs = [ "docker-login", "master-branch-filter" ]
    uses = "actions/docker/cli@master"
    args = "build -t naiba/solitudes ."
}

action "build-tag" {
    needs = [ "docker-login", "tag-filter" ]
    uses = "actions/docker/cli@master"
    args = "build -t naiba/solitudes:$GITHUB_REF ."
}