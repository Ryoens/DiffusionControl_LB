# Usage: make [task name] [number]
ARG=$(filter-out $@,$(MAKECMDGOALS))-1
build: 
	echo $(ARG)
	@cd cmd && ./DockerBuild.sh $(ARG)
destroy:
	echo $(ARG)
	@cd cmd && ./DockerDestroy.sh $(ARG)
exec:
	@cd cmd && ./Execute.sh
web:
	@cd cmd && ./ExecuteWebUI.sh
%:
	@:
