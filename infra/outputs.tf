output "app_url" {
  value       = "https://${aws_cloudfront_distribution.app.domain_name}"
  description = "Public URL for the React application."
}

output "api_url" {
  value       = aws_apigatewayv2_api.http.api_endpoint
  description = "HTTP API base URL."
}

output "cognito_user_pool_id" {
  value       = aws_cognito_user_pool.users.id
  description = "Cognito User Pool ID."
}

output "cognito_client_id" {
  value       = aws_cognito_user_pool_client.web.id
  description = "Cognito public web client ID."
}

output "cognito_login_url" {
  value = "https://${aws_cognito_user_pool_domain.login.domain}.auth.${var.aws_region}.amazoncognito.com/login?client_id=${aws_cognito_user_pool_client.web.id}&response_type=code&scope=openid+email+profile&redirect_uri=https%3A%2F%2F${aws_cloudfront_distribution.app.domain_name}%2Fauth%2Fcallback"
}

output "cloudfront_distribution_id" {
  value       = aws_cloudfront_distribution.app.id
  description = "CloudFront distribution ID, for optional cache invalidation."
}

output "cognito_oidc_issuer" {
  value       = "https://cognito-idp.${var.aws_region}.amazonaws.com/${aws_cognito_user_pool.users.id}"
  description = "OIDC issuer/discovery authority used by the React OIDC client."
}
