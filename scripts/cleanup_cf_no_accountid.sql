-- cleanup_cf_no_accountid.sql
-- Hapus semua koneksi Cloudflare yang tidak punya accountId
-- Jalankan di production SQLite: sqlite3 /path/to/axonrouter.db < cleanup_cf_no_accountid.sql

-- 1. Hitung yang akan dihapus
SELECT '=== SEBELUM CLEANUP ===' as info;
SELECT COUNT(*) as total_cf_connections FROM connections WHERE provider_type_id = 'cf';
SELECT COUNT(*) as cf_without_accountid FROM connections 
WHERE provider_type_id = 'cf' 
AND (
  provider_specific_data IS NULL 
  OR provider_specific_data = '' 
  OR json_extract(provider_specific_data, '$.accountId') IS NULL
  OR TRIM(json_extract(provider_specific_data, '$.accountId')) = ''
);

-- 2. Sample data yang akan dihapus
SELECT '=== SAMPLE (5 rows) ===' as info;
SELECT id, name, SUBSTR(api_key, 1, 10) || '...' as api_key_prefix, 
       provider_specific_data, status, is_active
FROM connections 
WHERE provider_type_id = 'cf' 
AND (
  provider_specific_data IS NULL 
  OR provider_specific_data = '' 
  OR json_extract(provider_specific_data, '$.accountId') IS NULL
  OR TRIM(json_extract(provider_specific_data, '$.accountId')) = ''
)
LIMIT 5;

-- 3. Hapus
DELETE FROM connections 
WHERE provider_type_id = 'cf' 
AND (
  provider_specific_data IS NULL 
  OR provider_specific_data = '' 
  OR json_extract(provider_specific_data, '$.accountId') IS NULL
  OR TRIM(json_extract(provider_specific_data, '$.accountId')) = ''
);

-- 4. Verifikasi
SELECT '=== SETELAH CLEANUP ===' as info;
SELECT COUNT(*) as remaining_cf_connections FROM connections WHERE provider_type_id = 'cf';
SELECT 'Cleanup selesai!' as status;
