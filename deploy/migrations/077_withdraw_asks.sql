UPDATE acts SET state='withdrawn', closed_at=now()
WHERE kind='ask' AND state IN ('open','failed');
