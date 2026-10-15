DELETE FROM scheduler_capacity_locks WHERE name IN ('direct', 'browser');
DELETE FROM users WHERE role='admin' AND email=current_setting('monitoring_service.admin_email', true);
