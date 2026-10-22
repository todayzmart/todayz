TF := terraform

tf-destroy:
	$(TF) -chdir=infra/terraform destroy -auto-approve
