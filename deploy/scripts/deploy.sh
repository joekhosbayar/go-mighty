#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

TF=deploy/terraform
BUCKET=$(tofu -chdir="$TF" output -raw deploy_bucket)
INSTANCE_ID=$(tofu -chdir="$TF" output -raw instance_id)

aws s3 sync deploy/compose "s3://${BUCKET}/bundle/compose" --delete
aws s3 sync migrations "s3://${BUCKET}/bundle/migrations" --delete

# Parameters as strict JSON — shorthand parsing chokes on embedded quotes/newlines
PARAMS=$(printf '{"commands":["%s","%s","%s"]}' \
	"aws s3 sync s3://${BUCKET}/bundle/compose /opt/mighty --delete --region us-east-1" \
	"aws s3 sync s3://${BUCKET}/bundle/migrations /opt/mighty/migrations --delete --region us-east-1" \
	"bash /opt/mighty/remote-deploy.sh")

CMD_ID=$(aws ssm send-command \
	--region us-east-1 \
	--instance-ids "$INSTANCE_ID" \
	--document-name AWS-RunShellScript \
	--comment "mighty deploy" \
	--parameters "$PARAMS" \
	--query Command.CommandId --output text)

echo "Command: $CMD_ID — waiting..."
aws ssm wait command-executed --region us-east-1 \
	--command-id "$CMD_ID" --instance-id "$INSTANCE_ID" || true
aws ssm get-command-invocation --region us-east-1 \
	--command-id "$CMD_ID" --instance-id "$INSTANCE_ID" \
	--query '{Status:Status,Stdout:StandardOutputContent,Stderr:StandardErrorContent}' \
	--output json
