resource "aws_amplify_app" "frontend" {
  name       = "mighty-frontend"
  repository = "https://github.com/joekhosbayar/mighty-frontend"

  # The GitHub token is required for Amplify to set up webhooks and pull code
  access_token = var.github_token

  # Redirect all client-side routed paths to index.html to avoid 404s
  custom_rule {
    source = "</^[^.]+$|\\.(?!(css|gif|ico|jpg|js|png|txt|svg|woff|woff2|ttf|map|json)$)([^.]+$)/>"
    status = "200"
    target = "/index.html"
  }

  environment_variables = {
    AMPLIFY_MONOREPO_APP_ROOT = "mighty-frontend"
    VITE_COGNITO_REGION       = "us-east-1"
    VITE_COGNITO_POOL_ID      = aws_ssm_parameter.cognito_pool_id.value
    VITE_COGNITO_CLIENT_ID    = aws_ssm_parameter.cognito_client_id.value
    VITE_API_URL              = "https://api.${var.domain}"
  }
}

resource "aws_amplify_branch" "main" {
  app_id      = aws_amplify_app.frontend.id
  branch_name = "main"
  framework   = "React"
}

resource "aws_amplify_domain_association" "frontend" {
  app_id      = aws_amplify_app.frontend.id
  domain_name = var.domain

  sub_domain {
    branch_name = aws_amplify_branch.main.branch_name
    prefix      = "app"
  }
}
