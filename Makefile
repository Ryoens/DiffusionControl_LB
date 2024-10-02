# Usage: make [タスク名] [数値]
ARG=$(filter-out $@,$(MAKECMDGOALS))
build: 
	echo $(ARG)
	@cd cmd && ./DockerBuild.sh $(ARG)
destroy:
	echo $(ARG)
	@cd cmd && ./DockerDestroy.sh $(ARG)
%:
	@:
