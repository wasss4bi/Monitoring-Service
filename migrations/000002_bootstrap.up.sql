DO $$
DECLARE
 admin_email TEXT := current_setting('monitoring_service.admin_email',true);
 password_hash TEXT := current_setting('monitoring_service.admin_password_hash',true);
BEGIN
 IF COALESCE(admin_email,'')='' OR COALESCE(password_hash,'') !~ '^\$2[aby]\$[0-9]{2}\$' THEN
  RAISE EXCEPTION 'administrator bootstrap settings are missing';
 END IF;
 IF EXISTS(SELECT 1 FROM users WHERE email=admin_email) THEN
  RAISE EXCEPTION 'administrator email conflicts with an existing user';
 END IF;
 INSERT INTO users(name,email,password,role) VALUES('Administrator',admin_email,password_hash,'admin');
END $$;

INSERT INTO scheduler_capacity_locks(name) VALUES ('direct'),('browser');
