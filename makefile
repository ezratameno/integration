docker-run:
	docker run -d -p 3000:3000 -p 2222:22  -e GITEA__security__INSTALL_LOCK=true --name gitea gitea/gitea:1.21.7

docker-remove:
	docker container rm -f gitea

docker-restart: docker-remove docker-run
tidy:
	go mod tidy


Cluster="test23"
run: pull-images
	@go run ./cmd/cli/integration_client/ create --kustomizations flux-system/apps  \
	--cluster ${Cluster} \
	--local-repo ~/etameno/Desktop/github/habana-k8s-infra-services \
	--flux-path flux/clusters/dc02 \
	--manifests https://raw.githubusercontent.com/kubernetes-sigs/scheduler-plugins/release-1.23/manifests/capacityscheduling/crd.yaml \
	--kind-config /home/etameno/etameno/Desktop/github/habana-k8s-infra-services/test/kind-cluster/kind-cluster.yaml \
	--kind-images ghcr.io/fluxcd/helm-controller:v0.31.2,ghcr.io/fluxcd/kustomize-controller:v0.35.1,ghcr.io/fluxcd/notification-controller:v0.33.0,ghcr.io/fluxcd/source-controller:v0.36.1

delete:
	go run ./cmd/cli/integration_client/ delete --cluster ${Cluster}


pull-images:
	docker pull ghcr.io/fluxcd/helm-controller:v0.31.2
	docker pull ghcr.io/fluxcd/kustomize-controller:v0.35.1
	docker pull ghcr.io/fluxcd/notification-controller:v0.33.0
	docker pull ghcr.io/fluxcd/source-controller:v0.36.1