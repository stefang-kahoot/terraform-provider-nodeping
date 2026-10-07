# Example: Using notification dependency (dep) to reduce alert noise
#
# This example shows how to configure a dependent check that will suppress
# notifications when a parent check (e.g., router or core service) is failing.

terraform {
  required_providers {
    nodeping = {
      source  = "stefang-kahoot/nodeping"
      version = "~> 1.0"
    }
  }
}

provider "nodeping" {
  # API token can be set via NODEPING_API_TOKEN environment variable
  # api_token = var.nodeping_api_token
}

# Primary check - monitors the edge router
resource "nodeping_check" "edge_router" {
  type    = "PING"
  target  = "192.168.1.1"
  label   = "Edge Router"
  enabled = true

  interval  = 1  # Check every minute
  threshold = 5  # 5 second timeout
  sens      = 2  # 2 rechecks before status change

  runlocations = ["nam"]
}

# Dependent check - web server behind the router
# Notifications will be suppressed if the router check is failing
resource "nodeping_check" "web_server" {
  type    = "HTTP"
  target  = "https://internal.example.com"
  label   = "Internal Web Server"
  enabled = true

  interval  = 5
  threshold = 10
  sens      = 2

  # Set notification dependency to router check
  # If router is down, no notifications will be sent for this check
  dep = nodeping_check.edge_router.id

  runlocations = ["nam"]
}

# Another dependent check - database server
resource "nodeping_check" "database" {
  type     = "PORT"
  target   = "db.internal.example.com"
  label    = "Database Server"
  enabled  = true
  port     = 5432

  interval  = 5
  threshold = 10
  sens      = 2

  # Also depends on the router
  dep = nodeping_check.edge_router.id

  runlocations = ["nam"]
}

# Output the check IDs
output "router_check_id" {
  value       = nodeping_check.edge_router.id
  description = "The ID of the edge router check"
}

output "web_server_check_id" {
  value       = nodeping_check.web_server.id
  description = "The ID of the web server check (depends on router)"
}

output "database_check_id" {
  value       = nodeping_check.database.id
  description = "The ID of the database check (depends on router)"
}
