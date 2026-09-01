SELECT 'CREATE DATABASE gin_monolith_test'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'gin_monolith_test')\gexec
