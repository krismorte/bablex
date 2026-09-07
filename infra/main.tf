locals {
  name = "${var.project_name}-${var.environment}"
  tags = {
    Project     = var.project_name
    Environment = var.environment
    ManagedBy   = "terraform"
  }

  frontend_files = { for key in fileset(var.frontend_dist_dir, "**") : key => key if key != "config.json" }
}

resource "aws_dynamodb_table" "library" {
  name         = "${local.name}-library"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "PK"
  range_key    = "SK"

  attribute {
    name = "PK"
    type = "S"
  }

  attribute {
    name = "SK"
    type = "S"
  }

  point_in_time_recovery {
    enabled = true
  }

  server_side_encryption {
    enabled = true
  }

  tags = local.tags
}

resource "aws_cognito_user_pool" "users" {
  name = "${local.name}-users"

  username_attributes      = ["email"]
  auto_verified_attributes = ["email"]

  password_policy {
    minimum_length                   = 8
    require_lowercase                = true
    require_uppercase                = true
    require_numbers                  = true
    require_symbols                  = false
    temporary_password_validity_days = 7
  }

  account_recovery_setting {
    recovery_mechanism {
      name     = "verified_email"
      priority = 1
    }
  }

  verification_message_template {
    default_email_option = "CONFIRM_WITH_CODE"
  }

  tags = local.tags
}

resource "aws_cognito_user_pool_domain" "login" {
  domain       = var.cognito_domain_prefix
  user_pool_id = aws_cognito_user_pool.users.id
}

resource "aws_cognito_user_pool_client" "web" {
  name         = "${local.name}-web"
  user_pool_id = aws_cognito_user_pool.users.id

  generate_secret = false

  allowed_oauth_flows_user_pool_client = true
  allowed_oauth_flows                  = ["code"]
  allowed_oauth_scopes                 = ["openid", "email", "profile"]
  supported_identity_providers         = ["COGNITO"]

  callback_urls = [
    "https://${aws_cloudfront_distribution.app.domain_name}/auth/callback"
  ]

  logout_urls = [
    "https://${aws_cloudfront_distribution.app.domain_name}/"
  ]

  explicit_auth_flows = [
    "ALLOW_REFRESH_TOKEN_AUTH"
  ]

  depends_on = [aws_cloudfront_distribution.app]
}

resource "aws_iam_role" "lambda" {
  name = "${local.name}-lambda-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Principal = {
        Service = "lambda.amazonaws.com"
      }
      Action = "sts:AssumeRole"
    }]
  })

  tags = local.tags
}

resource "aws_iam_role_policy" "lambda" {
  name = "${local.name}-lambda-policy"
  role = aws_iam_role.lambda.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "DynamoDB"
        Effect = "Allow"
        Action = [
          "dynamodb:GetItem",
          "dynamodb:PutItem",
          "dynamodb:UpdateItem",
          "dynamodb:DeleteItem",
          "dynamodb:Query",
          "dynamodb:TransactWriteItems"
        ]
        Resource = aws_dynamodb_table.library.arn
      },
      {
        Sid    = "Logs"
        Effect = "Allow"
        Action = [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents"
        ]
        Resource = "arn:aws:logs:*:*:*"
      }
    ]
  })
}

resource "aws_cloudwatch_log_group" "lambda" {
  name              = "/aws/lambda/${local.name}-api"
  retention_in_days = 14
  tags              = local.tags
}

resource "aws_lambda_function" "api" {
  function_name = "${local.name}-api"
  role          = aws_iam_role.lambda.arn
  runtime       = "provided.al2023"
  handler       = "bootstrap"

  filename         = "lambda.zip"
  source_code_hash = filebase64sha256("lambda.zip")

  memory_size   = 256
  timeout       = 10
  architectures = ["arm64"]

  environment {
    variables = {
      TABLE_NAME = aws_dynamodb_table.library.name
      LOG_LEVEL  = "INFO"
      AUTH_MODE  = "aws"
    }
  }

  depends_on = [aws_cloudwatch_log_group.lambda]
  tags       = local.tags
}

resource "aws_apigatewayv2_api" "http" {
  name          = "${local.name}-http"
  protocol_type = "HTTP"


  cors_configuration {
    allow_origins = ["*"]
    allow_methods = ["DELETE", "GET", "OPTIONS", "PATCH", "POST"]
    allow_headers = ["authorization", "content-type"]
    max_age       = 300
  }


  tags = local.tags
}

resource "aws_apigatewayv2_authorizer" "cognito" {
  api_id           = aws_apigatewayv2_api.http.id
  authorizer_type  = "JWT"
  identity_sources = ["$request.header.Authorization"]
  name             = "${local.name}-cognito"

  jwt_configuration {
    audience = [aws_cognito_user_pool_client.web.id]
    issuer   = "https://cognito-idp.${var.aws_region}.amazonaws.com/${aws_cognito_user_pool.users.id}"
  }
}

resource "aws_lambda_permission" "api" {
  statement_id  = "AllowApiGatewayInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.api.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.http.execution_arn}/*/*"
}

resource "aws_apigatewayv2_integration" "api" {
  api_id                 = aws_apigatewayv2_api.http.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_function.api.invoke_arn
  integration_method     = "POST"
  payload_format_version = "2.0"
}

resource "aws_apigatewayv2_route" "health" {
  api_id    = aws_apigatewayv2_api.http.id
  route_key = "GET /health"
  target    = "integrations/${aws_apigatewayv2_integration.api.id}"
}

resource "aws_apigatewayv2_route" "me_get" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /me"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "me_any" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "POST /me/password"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "me_patch" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "PATCH /me"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "libraries_get" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /libraries"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "libraries_post" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "POST /libraries"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "libraries_patch" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "PATCH /libraries"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "libraries_delete" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "DELETE /libraries"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "library_get" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /libraries/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "library_patch" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "PATCH /libraries/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "library_delete" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "DELETE /libraries/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "access_get" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /libraries/{id}/access"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "share_post" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "POST /libraries/{id}/share"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "share_delete" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "DELETE /libraries/{id}/share"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "books_get" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /libraries/{id}/books"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "books_post" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "POST /libraries/{id}/books"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "books_patch" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "PATCH /libraries/{id}/books"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "book_get" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /libraries/{id}/books/{bookId}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "book_patch" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "PATCH /libraries/{id}/books/{bookId}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "book_delete" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "DELETE /libraries/{id}/books/{bookId}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "comments_get" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /libraries/{id}/books/{bookId}/comments"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "comments_post" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "POST /libraries/{id}/books/{bookId}/comments"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "move_post" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "POST /libraries/{id}/books/{bookId}/move"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "authors_get" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /authors"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "authors_post" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "POST /authors"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "authors_patch" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "PATCH /authors"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "authors_delete" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "DELETE /authors"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "author_get" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /authors/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "author_patch" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "PATCH /authors/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "author_delete" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "DELETE /authors/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "categories_get" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /categories"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "categories_post" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "POST /categories"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "categories_patch" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "PATCH /categories"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "categories_delete" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "DELETE /categories"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "category_get" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /categories/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "category_patch" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "PATCH /categories/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "category_delete" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "DELETE /categories/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_me_get" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /api/me"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_me_password" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "POST /api/me/password"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_me_patch" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "PATCH /api/me"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_libraries_get" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /api/libraries"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_libraries_post" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "POST /api/libraries"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_libraries_patch" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "PATCH /api/libraries"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_libraries_delete" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "DELETE /api/libraries"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_library_get" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /api/libraries/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_library_patch" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "PATCH /api/libraries/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_library_delete" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "DELETE /api/libraries/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_access_get" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /api/libraries/{id}/access"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_share_post" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "POST /api/libraries/{id}/share"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_share_delete" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "DELETE /api/libraries/{id}/share"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_books_get" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /api/libraries/{id}/books"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_books_post" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "POST /api/libraries/{id}/books"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_books_patch" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "PATCH /api/libraries/{id}/books"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_book_get" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /api/libraries/{id}/books/{bookId}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_book_patch" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "PATCH /api/libraries/{id}/books/{bookId}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_book_delete" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "DELETE /api/libraries/{id}/books/{bookId}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_comments_get" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /api/libraries/{id}/books/{bookId}/comments"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_comments_post" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "POST /api/libraries/{id}/books/{bookId}/comments"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_move_post" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "POST /api/libraries/{id}/books/{bookId}/move"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_authors_get" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /api/authors"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_authors_post" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "POST /api/authors"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_authors_patch" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "PATCH /api/authors"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_authors_delete" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "DELETE /api/authors"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_author_get" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /api/authors/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_author_patch" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "PATCH /api/authors/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_author_delete" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "DELETE /api/authors/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_categories_get" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /api/categories"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_categories_post" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "POST /api/categories"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_categories_patch" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "PATCH /api/categories"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_categories_delete" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "DELETE /api/categories"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_category_get" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /api/categories/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_category_patch" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "PATCH /api/categories/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "api_category_delete" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "DELETE /api/categories/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_apigatewayv2_route" "options_0" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /me"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "options_1" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /libraries"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "options_2" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /libraries/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "options_3" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /libraries/{id}/access"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "options_4" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /libraries/{id}/share"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "options_5" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /libraries/{id}/books"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "options_6" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /libraries/{id}/books/{bookId}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "options_7" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /libraries/{id}/books/{bookId}/comments"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "options_8" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /authors"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "options_9" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /authors/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "options_10" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /categories"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "options_11" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /categories/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "options_12" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /api/me"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "options_13" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /api/libraries"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "options_14" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /api/libraries/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "options_15" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /api/libraries/{id}/access"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "options_16" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /api/libraries/{id}/share"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "options_17" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /api/libraries/{id}/books"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "options_18" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /api/libraries/{id}/books/{bookId}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "options_19" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /api/libraries/{id}/books/{bookId}/comments"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "options_20" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /api/authors"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "options_21" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /api/authors/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "options_22" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /api/categories"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "options_23" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "OPTIONS /api/categories/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_stage" "default" {
  api_id      = aws_apigatewayv2_api.http.id
  name        = "$default"
  auto_deploy = true

  access_log_settings {
    destination_arn = aws_cloudwatch_log_group.apigw.arn
    format = jsonencode({
      requestId               = "$context.requestId"
      requestTime             = "$context.requestTime"
      httpMethod              = "$context.httpMethod"
      routeKey                = "$context.routeKey"
      status                  = "$context.status"
      responseLength          = "$context.responseLength"
      integrationErrorMessage = "$context.integrationErrorMessage"
    })
  }

  tags = local.tags
}

resource "aws_cloudwatch_log_group" "apigw" {
  name              = "/aws/apigateway/${local.name}"
  retention_in_days = 14
  tags              = local.tags
}

resource "aws_s3_bucket" "frontend" {
  bucket_prefix = "${local.name}-web-"
  tags          = local.tags
}

resource "aws_s3_bucket_public_access_block" "frontend" {
  bucket = aws_s3_bucket.frontend.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_versioning" "frontend" {
  bucket = aws_s3_bucket.frontend.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "frontend" {
  bucket = aws_s3_bucket.frontend.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_cloudfront_origin_access_control" "frontend" {
  name                              = "${local.name}-oac"
  description                       = "CloudFront access to private S3 frontend"
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

resource "aws_wafv2_ip_set" "allowed_clients" {
  provider           = aws.global
  name               = "${local.name}-allowed-clients"
  description        = "Public IPv4 addresses allowed to access Bablex"
  scope              = "CLOUDFRONT"
  ip_address_version = "IPV4"
  addresses          = var.allowed_public_ipv4_cidrs
  tags               = local.tags
}

resource "aws_wafv2_web_acl" "app" {
  provider = aws.global
  name     = "${local.name}-ip-allowlist"
  scope    = "CLOUDFRONT"

  default_action {
    block {}
  }

  rule {
    name     = "AllowConfiguredIPv4"
    priority = 1

    action {
      allow {}
    }

    statement {
      ip_set_reference_statement {
        arn = aws_wafv2_ip_set.allowed_clients.arn
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${local.name}-allow-ip"
      sampled_requests_enabled   = true
    }
  }

  visibility_config {
    cloudwatch_metrics_enabled = true
    metric_name                = "${local.name}-ip-allowlist"
    sampled_requests_enabled   = true
  }

  tags = local.tags
}

resource "aws_cloudfront_function" "spa_rewrite" {
  name    = "${local.name}-spa-rewrite"
  runtime = "cloudfront-js-1.0"
  comment = "Rewrite Bablex SPA routes to index.html"
  publish = true

  code = <<-EOF
function handler(event) {
  var request = event.request;
  var uri = request.uri;
  if (uri === "/" || !uri.includes(".")) {
    request.uri = "/index.html";
  }
  return request;
}
EOF

  tags = local.tags
}

resource "aws_cloudfront_distribution" "app" {
  enabled             = true
  default_root_object = "index.html"
  comment             = local.name
  web_acl_id          = aws_wafv2_web_acl.app.arn

  origin {
    domain_name              = aws_s3_bucket.frontend.bucket_regional_domain_name
    origin_id                = "s3-${aws_s3_bucket.frontend.id}"
    origin_access_control_id = aws_cloudfront_origin_access_control.frontend.id
  }

  origin {
    domain_name = replace(aws_apigatewayv2_api.http.api_endpoint, "https://", "")
    origin_id   = "api-${aws_apigatewayv2_api.http.id}"

    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = "https-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }

  ordered_cache_behavior {
    path_pattern           = "/api/*"
    target_origin_id       = "api-${aws_apigatewayv2_api.http.id}"
    viewer_protocol_policy = "redirect-to-https"

    allowed_methods = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
    cached_methods  = ["GET", "HEAD", "OPTIONS"]

    forwarded_values {
      query_string = true
      headers      = ["Authorization", "Content-Type", "Origin", "Access-Control-Request-Headers", "Access-Control-Request-Method"]
      cookies {
        forward = "none"
      }
    }

    min_ttl     = 0
    default_ttl = 0
    max_ttl     = 0
  }

  default_cache_behavior {
    target_origin_id       = "s3-${aws_s3_bucket.frontend.id}"
    viewer_protocol_policy = "redirect-to-https"

    allowed_methods = ["GET", "HEAD", "OPTIONS"]
    cached_methods  = ["GET", "HEAD"]

    function_association {
      event_type   = "viewer-request"
      function_arn = aws_cloudfront_function.spa_rewrite.arn
    }

    forwarded_values {
      query_string = true
      cookies {
        forward = "none"
      }
    }
  }


  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  viewer_certificate {
    cloudfront_default_certificate = true
  }

  tags = local.tags
}

resource "aws_s3_bucket_policy" "frontend" {
  bucket = aws_s3_bucket.frontend.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid    = "AllowCloudFrontServicePrincipalReadOnly"
      Effect = "Allow"
      Principal = {
        Service = "cloudfront.amazonaws.com"
      }
      Action   = "s3:GetObject"
      Resource = "${aws_s3_bucket.frontend.arn}/*"
      Condition = {
        StringEquals = {
          "AWS:SourceArn" = aws_cloudfront_distribution.app.arn
        }
      }
    }]
  })
}

resource "aws_s3_object" "frontend" {
  for_each = local.frontend_files

  bucket = aws_s3_bucket.frontend.id
  key    = each.value
  source = "${var.frontend_dist_dir}/${each.value}"
  etag   = filemd5("${var.frontend_dist_dir}/${each.value}")

  content_type = lookup({
    html = "text/html"
    css  = "text/css"
    js   = "application/javascript"
    json = "application/json"
    svg  = "image/svg+xml"
    png  = "image/png"
    jpg  = "image/jpeg"
    jpeg = "image/jpeg"
    ico  = "image/x-icon"
    webp = "image/webp"
    txt  = "text/plain"
  }, split(".", each.value)[length(split(".", each.value)) - 1], "application/octet-stream")

  cache_control = each.value == "index.html" || each.value == "config.json" ? "no-cache, no-store, must-revalidate" : "public,max-age=31536000,immutable"

  depends_on = [aws_s3_bucket_policy.frontend]
}

resource "aws_s3_object" "runtime_config" {
  bucket = aws_s3_bucket.frontend.id
  key    = "config.json"

  content = jsonencode({
    mode             = "aws"
    apiBaseUrl       = "/api"
    cognitoAuthority = "https://cognito-idp.${var.aws_region}.amazonaws.com/${aws_cognito_user_pool.users.id}"
    cognitoClientId  = aws_cognito_user_pool_client.web.id
    redirectUri      = "https://${aws_cloudfront_distribution.app.domain_name}/auth/callback"
    logoutUri        = "https://${aws_cloudfront_distribution.app.domain_name}/"
  })

  content_type  = "application/json"
  cache_control = "no-cache, no-store, must-revalidate"

  depends_on = [aws_s3_bucket_policy.frontend]
}
