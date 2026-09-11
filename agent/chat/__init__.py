#!/usr/bin/env python3
"""Chat of a claude session: transcript parsing and windowed delivery over a socket."""
import os

import paths

SOCKET_DIR = os.environ.get("AACP_CHAT_DIR", paths.state("chat"))
SOCKET_NAME = "chat.sock"

PROJECTS_DIR = os.environ.get("AACP_CLAUDE_PROJECTS")

from .cards import (ASK_REJECTED, MAX_ASK_ANSWERS, MAX_ASK_QUESTIONS,  # noqa: E402,F401
                    MAX_ASK_TEXT, artifact_card, ask_round, wake_item)
from .disk import (MAX_FILE, MAX_FILES, MAX_MEDIA, MAX_PROBE, MAX_RAW,  # noqa: E402,F401
                   TASK_ID_RE, TASK_TAIL, attach_files, as_is, is_exec,
                   media_of, named_files, read_file, read_raw, task_output,
                   trim_utf8)
from .harness import (COMMAND_RE, NOTES, PANEL_NOTE, SKIP, TASK_FIELD_RE,  # noqa: E402,F401
                      TASK_NOTE_RE, classify, service, strip_panel_note, task_done)
from .limits import (DEFAULT_LIMIT, MAX_ARG, MAX_ARGS, MAX_LIMIT,  # noqa: E402,F401
                     MAX_RESULT, MAX_TEXT, cut)
from .locate import (SUBAGENT_ID_RE, UUID_RE, dirs_for, profile_dirs,  # noqa: E402,F401
                     subagent_path, transcript_cwd, transcript_path)
from .mail import MAIL_ATTR_RE, MAIL_RE, agent_mail, mails  # noqa: E402,F401
from .queue import Pending, delivered  # noqa: E402,F401
from .records import parse  # noqa: E402,F401
from .server import (MAX_REQUEST, answer, archive_index, handle, listen,  # noqa: E402,F401
                     observe_names, serve, worker)
from .spots import blocks_of, call, image, tool_result  # noqa: E402,F401
from .tools import ARG_KEYS, TOOL_KINDS, one_line, tool_arg, tool_kind, tool_label  # noqa: E402,F401
from .window import feed  # noqa: E402,F401
