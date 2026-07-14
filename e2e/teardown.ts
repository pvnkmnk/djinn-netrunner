import { execSync } from 'child_process';

export default async function globalTeardown() {
  if (!process.env.CI) {
    console.log('Skipping Docker teardown in local dev (reuseExistingServer is enabled).');
    return;
  }
  console.log('Tearing down Docker stack...');
  execSync('docker compose --env-file ../.env.e2e -f ../docker-compose.yml -f ../docker-compose.e2e.yml down -v --remove-orphans', { stdio: 'inherit' });
}
