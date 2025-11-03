# Add region variable for flexibility
variable "aws_region" {
  default = "us-east-1"
}

# Terraform config to deploy Go API as AWS Lambda + expose it via API Gateway

provider "aws" {
  region = var.aws_region
}

resource "aws_iam_role" "lambda_exec_role" {
  name = "lambda_exec_role_strukturbild${local.name_suffix}"

  assume_role_policy = jsonencode({
    Version = "2012-10-17",
    Statement = [
      {
        Action = "sts:AssumeRole",
        Principal = {
          Service = "lambda.amazonaws.com"
        },
        Effect = "Allow",
        Sid    = ""
      }
    ]
  })
}

resource "aws_iam_role_policy_attachment" "lambda_logs" {
  role       = aws_iam_role.lambda_exec_role.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_lambda_function" "strukturbild_api" {
  function_name = "strukturbild-api${local.name_suffix}"
  role          = aws_iam_role.lambda_exec_role.arn
  handler       = "bootstrap"
  runtime       = "provided.al2023"
  filename      = "bootstrap.zip"  # This should be your Go binary zipped as 'bootstrap'
  source_code_hash = filebase64sha256("bootstrap.zip")
  timeout       = 10
  environment {
    variables = {
      ENV        = local.env
      TABLE_NAME = local.env == "prod" ? "strukturbild_data" : "strukturbild_data_${local.env}"
    }
  }
}

resource "aws_apigatewayv2_api" "http_api" {
  name          = "strukturbild-http-api${local.name_suffix}"
  protocol_type = "HTTP"
  cors_configuration {
    allow_origins     = ["*"]
    allow_methods     = ["OPTIONS", "GET", "POST", "DELETE"]
    allow_headers     = [
      "content-type",
      "authorization",
      "x-requested-with",
      "x-amz-date",
      "x-api-key",
      "x-amz-security-token"
    ]
    expose_headers    = []
    max_age           = 86400
    allow_credentials = false
  }
}

resource "aws_lambda_permission" "apigw" {
  statement_id  = "AllowAPIGatewayInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.strukturbild_api.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.http_api.execution_arn}/*/*"
}

resource "aws_lambda_permission" "apigw_get" {
  statement_id  = "AllowAPIGatewayInvokeGet"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.strukturbild_api.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.http_api.execution_arn}/GET/struktur/*"
}

resource "aws_apigatewayv2_integration" "lambda_integration" {
  api_id             = aws_apigatewayv2_api.http_api.id
  integration_type   = "AWS_PROXY"
  integration_uri    = aws_lambda_function.strukturbild_api.invoke_arn
  integration_method = "POST"
  payload_format_version = "1.0"
}

resource "aws_apigatewayv2_route" "submit_route" {
  api_id    = aws_apigatewayv2_api.http_api.id
  route_key = "POST /submit"
  target    = "integrations/${aws_apigatewayv2_integration.lambda_integration.id}"
}


resource "aws_apigatewayv2_deployment" "deployment" {
  api_id = aws_apigatewayv2_api.http_api.id

  depends_on = [
    aws_apigatewayv2_integration.lambda_integration,
    aws_apigatewayv2_route.submit_route,
    aws_apigatewayv2_route.get_route,
    aws_apigatewayv2_route.delete_route,
  ]
}
resource "aws_apigatewayv2_route" "delete_route" {
  api_id             = aws_apigatewayv2_api.http_api.id
  route_key          = "DELETE /struktur/{storyId}/{nodeId}"
  target             = "integrations/${aws_apigatewayv2_integration.lambda_integration.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_stage" "default" {
  api_id      = aws_apigatewayv2_api.http_api.id
  name        = local.stage_name
  auto_deploy = true
}

output "api_url" {
  value = aws_apigatewayv2_stage.default.invoke_url
}

output "http_api_id" {
  value = aws_apigatewayv2_api.http_api.id
}

# Static frontend S3 hosting

resource "aws_s3_bucket" "frontend_bucket" {
  bucket        = local.env == "prod" ? "strukturbild-frontend-a9141bf9" : "strukturbild-frontend-${local.env}-a9141bf9"
  force_destroy = true
}

resource "aws_s3_bucket_website_configuration" "frontend" {
  bucket = aws_s3_bucket.frontend_bucket.id

  index_document {
    suffix = "index.html"
  }
}

resource "aws_s3_bucket_public_access_block" "public_access" {
  bucket = aws_s3_bucket.frontend_bucket.id

  block_public_acls       = false
  block_public_policy     = false
  ignore_public_acls      = false
  restrict_public_buckets = false
}

resource "aws_s3_bucket_policy" "allow_public_read" {
  bucket = aws_s3_bucket.frontend_bucket.id

  policy = jsonencode({
    Version = "2012-10-17",
    Statement = [
      {
        Sid       = "PublicReadGetObject",
        Effect    = "Allow",
        Principal = "*",
        Action    = ["s3:GetObject"],
        Resource  = ["${aws_s3_bucket.frontend_bucket.arn}/*"]
      }
    ]
  })
}

resource "aws_s3_object" "frontend_files" {
  for_each = fileset("../frontend", "**/*")

  bucket = aws_s3_bucket.frontend_bucket.id
  key    = each.value
  source = "../frontend/${each.value}"
  content_type = lookup(
    {
      "html" = "text/html"
      "css"  = "text/css"
      "js"   = "application/javascript"
    },
    split(".", each.value)[length(split(".", each.value)) - 1],
    "application/octet-stream"
  )
}

resource "aws_dynamodb_table" "struktur_data" {
  name             = local.env == "prod" ? "strukturbild_data" : "strukturbild_data_${local.env}"
  billing_mode     = "PAY_PER_REQUEST"
  hash_key         = "storyId"
  range_key        = "id"

  attribute {
    name = "storyId"
    type = "S"
  }

  attribute {
    name = "id"
    type = "S"
  }

  tags = {
    Project = "strukturbild"
  }
}

resource "aws_iam_policy" "dynamodb_access" {
  name        = "LambdaDynamoDBAccess${local.name_suffix}"
  description = "Allow lambda to put items to DynamoDB"
  policy      = jsonencode({
    Version = "2012-10-17",
    Statement = [{
      Action = [
        "dynamodb:PutItem",
        "dynamodb:UpdateItem",
        "dynamodb:GetItem",
        "dynamodb:Query",
        "dynamodb:DeleteItem",
        "dynamodb:Scan"
      ],
      Effect   = "Allow",
      Resource = aws_dynamodb_table.struktur_data.arn
    }]
  })
}

resource "aws_iam_policy_attachment" "lambda_dynamodb_attach" {
  name       = "attach-lambda-dynamodb${local.name_suffix}"
  roles      = [aws_iam_role.lambda_exec_role.name]
  policy_arn = aws_iam_policy.dynamodb_access.arn
}

output "frontend_url" {
  value = aws_s3_bucket_website_configuration.frontend.website_endpoint
}

resource "aws_apigatewayv2_route" "get_route" {
  api_id             = aws_apigatewayv2_api.http_api.id
  route_key          = "GET /struktur/{id}"
  target             = "integrations/${aws_apigatewayv2_integration.lambda_integration.id}"
  authorization_type = "NONE"
}

resource "aws_apigatewayv2_route" "api_proxy" {
  api_id             = aws_apigatewayv2_api.http_api.id
  route_key          = "ANY /api/{proxy+}"
  authorization_type = "NONE"
  target             = "integrations/${aws_apigatewayv2_integration.lambda_integration.id}"
}