import { execSync } from 'child_process';

export default async function globalTeardown() {
  console.log('Tearing down Docker stack...');
  execSync('docker compose --env-file ../.env.e2e -f ../docker-compose.yml -f ../docker-compose.e2e.yml down -v --remove-orphans', { stdio: 'inherit' });
}
