ALTER TABLE profile_groups
    ADD CONSTRAINT profile_groups_profile_id_name_key UNIQUE (profile_id, name);

ALTER TABLE profile_projects
    ADD CONSTRAINT profile_projects_path_key UNIQUE (path);
