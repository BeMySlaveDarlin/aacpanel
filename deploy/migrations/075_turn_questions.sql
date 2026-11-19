ALTER TABLE text_questions
    DROP COLUMN answers,
    ADD COLUMN questions jsonb NOT NULL DEFAULT '[]',
    ADD COLUMN action text NOT NULL DEFAULT '',
    ADD COLUMN hooked boolean NOT NULL DEFAULT false;

UPDATE acts SET body = (body - 'answers') || jsonb_build_object('questions',
    jsonb_build_array(jsonb_build_object(
        'question', left(coalesce(nullif(body->>'what',''),title),200),
        'header', '', 'multi', false,
        'options', coalesce((SELECT jsonb_agg(jsonb_build_object('label',choice,'description',''))
            FROM jsonb_array_elements_text(body->'answers') AS choice), '[]'::jsonb))))
WHERE kind='ask' AND NOT body ? 'questions';

UPDATE text_questions q SET questions=a.body->'questions' FROM acts a
WHERE a.kind='ask' AND a.body->>'source'='prose'
    AND a.conversation=q.conversation AND a.body->>'message'=q.message;
