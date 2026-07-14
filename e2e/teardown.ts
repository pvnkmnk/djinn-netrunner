import { execSync } from 'child_process';

export default async function globalTeardown() {
  console.log('Tearing down Docker stack...');
  execSync('docker compose -f docker-compose.yml -f docker-compose.e2e.yml down -v --remove-orphans', { cwd: '/home/idols/orca/workspaces/netrunner_repo/auto-release-readiness-review-run-1-20260616T0835', stdio: 'inherit' });
}
