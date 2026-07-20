resource "aws_cognito_user_pool" "main" {
  name                = "mighty-users"
  user_pool_tier      = "ESSENTIALS"
  deletion_protection = "ACTIVE"

  # Sign in with email; Cognito generates an opaque internal username (== sub).
  username_attributes      = ["email"]
  auto_verified_attributes = ["email"]

  # USER_AUTH choice-based first factors (spec Section 2).
  sign_in_policy {
    allowed_first_auth_factors = ["PASSWORD", "WEB_AUTHN", "EMAIL_OTP"]
  }

  # Passkeys: RP id is the apex so app.themighty.gg can register credentials.
  web_authn_configuration {
    relying_party_id  = var.domain
    user_verification = "preferred"
  }

  mfa_configuration = "OPTIONAL"
  software_token_mfa_configuration {
    enabled = true
  }

  schema {
    name                = "preferred_username"
    attribute_data_type = "String"
    required            = true
    mutable             = true

    string_attribute_constraints {
      min_length = 1
      max_length = 128
    }
  }

  password_policy {
    minimum_length    = 8
    require_lowercase = true
    require_uppercase = true
    require_numbers   = true
    require_symbols   = false
  }

  account_recovery_setting {
    recovery_mechanism {
      name     = "verified_email"
      priority = 1
    }
  }
}

resource "aws_cognito_user_pool_client" "spa" {
  name         = "mighty-spa"
  user_pool_id = aws_cognito_user_pool.main.id

  # ADMIN_USER_PASSWORD_AUTH exists solely for CLI-scripted test users
  # (demo/e2e); remove when Plan 3's real signup UI is live.
  explicit_auth_flows = [
    "ALLOW_USER_AUTH",
    "ALLOW_USER_SRP_AUTH",
    "ALLOW_ADMIN_USER_PASSWORD_AUTH",
    "ALLOW_REFRESH_TOKEN_AUTH",
  ]

  generate_secret = false

  access_token_validity  = 60
  id_token_validity      = 60
  refresh_token_validity = 30

  token_validity_units {
    access_token  = "minutes"
    id_token      = "minutes"
    refresh_token = "days"
  }

  prevent_user_existence_errors = "ENABLED"
}
