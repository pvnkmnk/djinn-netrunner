#!/bin/bash
# Integration test runner for NetRunner
# This script manages dockerized slskd and runs integration tests

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Default values
COMPOSE_FILE="docker-compose.integration.yml"
PROJECT_NAME="netrunner-integration"
TIMEOUT="10m"
VERBOSE=""

# Help function
show_help() {
    echo "NetRunner Integration Test Runner"
    echo ""
    echo "Usage: $0 [command] [options]"
    echo ""
    echo "Commands:"
    echo "  up          Start integration services"
    echo "  down        Stop integration services"
    echo "  test        Run integration tests (starts services automatically)"
    echo "  logs        Show service logs"
    echo "  status      Check service status"
    echo "  clean       Clean up all integration test data"
    echo ""
    echo "Options:"
    echo "  -v          Verbose output"
    echo "  -t TIMEOUT  Test timeout (default: 10m)"
    echo "  -r TEST     Run specific test (e.g., TestSlskdEndToEndSearch)"
    echo "  -h          Show this help"
    echo ""
    echo "Environment Variables:"
    echo "  INTEGRATION_SLSKD_URL       slskd API URL (default: http://localhost:15030)"
    echo "  INTEGRATION_SLSKD_API_KEY   slskd API key (default: test-api-key-for-integration)"
    echo "  INTEGRATION_SLSKD_USERNAME  Soulseek username (optional, for download tests)"
    echo "  INTEGRATION_SLSKD_PASSWORD  Soulseek password (optional, for download tests)"
}

# Check dependencies
check_deps() {
    if ! command -v docker &> /dev/null; then
        echo -e "${RED}Error: Docker is not installed${NC}"
        exit 1
    fi
    
    if ! docker compose version &> /dev/null && ! docker-compose version &> /dev/null; then
        echo -e "${RED}Error: Docker Compose is not installed${NC}"
        exit 1
    fi
    
    if ! command -v go &> /dev/null; then
        echo -e "${RED}Error: Go is not installed${NC}"
        exit 1
    fi
}

# Start services
start_services() {
    echo -e "${YELLOW}Starting integration services...${NC}"
    
    if docker compose -f "$COMPOSE_FILE" -p "$PROJECT_NAME" up -d; then
        echo -e "${GREEN}Services started successfully${NC}"
    else
        echo -e "${RED}Failed to start services${NC}"
        exit 1
    fi
    
    echo -e "${YELLOW}Waiting for services to be healthy...${NC}"
    
    # Wait for slskd to be ready
    attempts=0
    max_attempts=60
    while [ $attempts -lt $max_attempts ]; do
        if curl -s http://localhost:15030/api/v0/session > /dev/null 2>&1; then
            echo -e "${GREEN}slskd is ready!${NC}"
            break
        fi
        attempts=$((attempts + 1))
        if [ $attempts -eq $max_attempts ]; then
            echo -e "${RED}Timeout waiting for slskd${NC}"
            echo -e "${YELLOW}Checking logs:${NC}"
            show_logs slskd-integration
            exit 1
        fi
        echo "Waiting for slskd... (attempt $attempts/$max_attempts)"
        sleep 2
    done
    
    # Wait for database
    attempts=0
    while [ $attempts -lt $max_attempts ]; do
        if docker compose -f "$COMPOSE_FILE" -p "$PROJECT_NAME" exec -T integration-db pg_isready -U testuser > /dev/null 2>&1; then
            echo -e "${GREEN}Database is ready!${NC}"
            break
        fi
        attempts=$((attempts + 1))
        if [ $attempts -eq $max_attempts ]; then
            echo -e "${RED}Timeout waiting for database${NC}"
            show_logs integration-db
            exit 1
        fi
        echo "Waiting for database... (attempt $attempts/$max_attempts)"
        sleep 2
    done
}

# Stop services
stop_services() {
    echo -e "${YELLOW}Stopping integration services...${NC}"
    docker compose -f "$COMPOSE_FILE" -p "$PROJECT_NAME" down -v
    echo -e "${GREEN}Services stopped${NC}"
}

# Show logs
show_logs() {
    service="$1"
    if [ -z "$service" ]; then
        docker compose -f "$COMPOSE_FILE" -p "$PROJECT_NAME" logs
    else
        docker compose -f "$COMPOSE_FILE" -p "$PROJECT_NAME" logs "$service"
    fi
}

# Check status
check_status() {
    echo -e "${YELLOW}Service Status:${NC}"
    docker compose -f "$COMPOSE_FILE" -p "$PROJECT_NAME" ps
    
    echo ""
    echo -e "${YELLOW}Checking slskd health...${NC}"
    if curl -s http://localhost:15030/api/v0/session > /dev/null 2>&1; then
        echo -e "${GREEN}slskd is healthy${NC}"
    else
        echo -e "${RED}slskd is not responding${NC}"
    fi
    
    echo ""
    echo -e "${YELLOW}Checking database...${NC}"
    if docker compose -f "$COMPOSE_FILE" -p "$PROJECT_NAME" exec -T integration-db pg_isready -U testuser > /dev/null 2>&1; then
        echo -e "${GREEN}Database is ready${NC}"
    else
        echo -e "${RED}Database is not ready${NC}"
    fi
}

# Run tests
run_tests() {
    specific_test="$1"
    
    # Check if services are running
    if ! curl -s http://localhost:15030/api/v0/session > /dev/null 2>&1; then
        echo -e "${YELLOW}Services not running, starting them...${NC}"
        start_services
    fi
    
    echo -e "${YELLOW}Running integration tests...${NC}"
    echo ""
    
    test_args="-v -tags=integration -timeout $TIMEOUT"
    if [ -n "$VERBOSE" ]; then
        test_args="$test_args -v"
    fi
    if [ -n "$specific_test" ]; then
        test_args="$test_args -run $specific_test"
    fi
    
    if go test ./backend/internal/integration/... $test_args; then
        echo ""
        echo -e "${GREEN}All integration tests passed!${NC}"
    else
        echo ""
        echo -e "${RED}Some integration tests failed${NC}"
        exit 1
    fi
}

# Clean up
clean_up() {
    echo -e "${YELLOW}Cleaning up integration test environment...${NC}"
    
    # Stop services
    docker compose -f "$COMPOSE_FILE" -p "$PROJECT_NAME" down -v 2>/dev/null || true
    
    # Remove volumes
    docker volume rm ${PROJECT_NAME}_slskd-integration-data 2>/dev/null || true
    docker volume rm ${PROJECT_NAME}_slskd-integration-downloads 2>/dev/null || true
    docker volume rm ${PROJECT_NAME}_integration-db-data 2>/dev/null || true
    
    echo -e "${GREEN}Cleanup complete${NC}"
}

# Main script
main() {
    COMMAND=""
    TEST_NAME=""
    
    # Parse arguments
    while [ $# -gt 0 ]; do
        case "$1" in
            up|down|test|logs|status|clean)
                COMMAND="$1"
                ;;
            -v)
                VERBOSE="-v"
                ;;
            -t)
                shift
                TIMEOUT="$1"
                ;;
            -r)
                shift
                TEST_NAME="$1"
                ;;
            -h|--help)
                show_help
                exit 0
                ;;
            *)
                echo -e "${RED}Unknown option: $1${NC}"
                show_help
                exit 1
                ;;
        esac
        shift
    done
    
    # Execute command
    case "$COMMAND" in
        up)
            check_deps
            start_services
            check_status
            ;;
        down)
            stop_services
            ;;
        test)
            check_deps
            run_tests "$TEST_NAME"
            ;;
        logs)
            show_logs "$2"
            ;;
        status)
            check_status
            ;;
        clean)
            clean_up
            ;;
        *)
            echo -e "${RED}No command specified${NC}"
            show_help
            exit 1
            ;;
    esac
}

main "$@"
