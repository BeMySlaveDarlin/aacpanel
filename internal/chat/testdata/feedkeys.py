"""Lists every place the collector writes a key into a dict, for the test of the fields of the feed.

A place is a dict literal, a key set by a subscript, a keyword of dict(...) or
of update(...), or a key written beside a spread; a literal returned names
its variable <return>. A subscript whose key is a
name bound by a for over a literal tuple writes the strings of the tuple; any
other name is a key nobody can read off the source, and the place says so.
Each place names its file, the outermost function it stands in and the
variable it writes to, and whether that variable is built in the function from
a literal with a role — a row of the feed.

    python3 feedkeys.py <file>... prints the places as one JSON list.
"""
import ast
import json
import os
import sys


def text(node):
    return node.value if isinstance(node, ast.Constant) and isinstance(node.value, str) else None


def literal_keys(node):
    return [k for k in (text(key) for key in node.keys if key is not None) if k is not None]


def is_row(node):
    return isinstance(node, ast.Dict) and "role" in literal_keys(node)


def target_name(node):
    """Returns the variable a dict is written into: a name, an attribute, or the one a subscript hangs on."""
    while isinstance(node, ast.Subscript):
        node = node.value
    if isinstance(node, ast.Attribute):
        return node.attr
    return node.id if isinstance(node, ast.Name) else ""


def loop_strings(node):
    """Returns the names a for binds to the strings of a literal tuple, each with the strings it takes."""
    if not isinstance(node.iter, (ast.Tuple, ast.List)):
        return {}
    paired = isinstance(node.target, ast.Tuple)
    targets = node.target.elts if paired else [node.target]
    names = {}
    for i, target in enumerate(targets):
        if not isinstance(target, ast.Name):
            continue
        values = []
        for elt in node.iter.elts:
            part = elt.elts[i] if paired and isinstance(elt, ast.Tuple) and i < len(elt.elts) else elt
            value = text(part)
            if value is None:
                values = None
                break
            values.append(value)
        if values is not None:
            names[target.id] = values
    return names


class Places(ast.NodeVisitor):
    def __init__(self, name):
        self.name = name
        self.function = ""
        self.rows = set()
        self.loops = []
        self.parents = {}
        self.out = []

    def place(self, node, var, keys, unknown=False):
        if not keys and not unknown:
            return
        self.out.append({"file": self.name, "function": self.function, "var": var,
                         "row": var in self.rows, "keys": sorted(set(keys)), "unknown": unknown,
                         "line": node.lineno})

    def bound(self, name):
        for names in reversed(self.loops):
            if name in names:
                return names[name]
        return None

    def visit_FunctionDef(self, node):
        if self.function:
            self.generic_visit(node)
            return
        self.function = node.name
        # The variables of the function built from a literal with a role.
        self.rows = {t.id for n in ast.walk(node) if isinstance(n, ast.Assign) and is_row(n.value)
                     for t in n.targets if isinstance(t, ast.Name)}
        self.generic_visit(node)
        self.function = ""
        self.rows = set()

    visit_AsyncFunctionDef = visit_FunctionDef

    def visit_For(self, node):
        self.loops.append(loop_strings(node))
        self.generic_visit(node)
        self.loops.pop()

    def visit_Dict(self, node):
        parent = self.parents.get(node)
        var = ""
        if isinstance(parent, ast.Assign):
            var = target_name(parent.targets[0])
        elif isinstance(parent, ast.Return):
            var = "<return>"
        spread = [target_name(v) for k, v in zip(node.keys, node.values) if k is None]
        if spread and spread[0]:
            var = spread[0]
        keys = literal_keys(node)
        self.out.append({"file": self.name, "function": self.function, "var": var,
                         "row": is_row(node) or var in self.rows, "keys": sorted(set(keys)),
                         "unknown": len(keys) + len(spread) != len(node.keys), "line": node.lineno,
                         "literal": True})
        self.generic_visit(node)

    def visit_Assign(self, node):
        for target in node.targets:
            if not isinstance(target, ast.Subscript):
                continue
            var = target_name(target.value)
            key = text(target.slice)
            if isinstance(target.slice, ast.Slice) or (isinstance(target.slice, ast.Constant) and key is None):
                # A place in a list, not a key.
                continue
            if key is not None:
                self.place(node, var, [key])
            elif isinstance(target.slice, ast.Name) and self.bound(target.slice.id) is not None:
                self.place(node, var, self.bound(target.slice.id))
            else:
                self.place(node, var, [], unknown=True)
        self.generic_visit(node)

    def visit_Call(self, node):
        func = node.func
        keywords = [k.arg for k in node.keywords if k.arg]
        spread = any(k.arg is None for k in node.keywords)
        if isinstance(func, ast.Name) and func.id == "dict" and (keywords or spread):
            var = target_name(node.args[0]) if node.args else ""
            self.place(node, var, keywords, unknown=False)
        if isinstance(func, ast.Attribute) and func.attr in ("update", "setdefault"):
            var = target_name(func.value)
            if func.attr == "update" and (keywords or node.args):
                self.place(node, var, keywords, unknown=bool(node.args) and not isinstance(node.args[0], ast.Dict))
            if func.attr == "setdefault" and node.args:
                key = text(node.args[0])
                self.place(node, var, [key] if key else [], unknown=key is None)
        self.generic_visit(node)


def places(path):
    tree = ast.parse(open(path, encoding="utf-8").read(), path)
    visitor = Places(os.path.basename(path))
    for parent in ast.walk(tree):
        for child in ast.iter_child_nodes(parent):
            visitor.parents[child] = parent
    visitor.visit(tree)
    return visitor.out


if __name__ == "__main__":
    json.dump([p for path in sys.argv[1:] for p in places(path)], sys.stdout)
