package parse

// Tree-sitter queries per grammar. Capture conventions:
//
//	@def.<kind>  the whole definition node (gives the line range)
//	@name        the definition's name
//	@recv        Go method receiver parameter list
//	@typebody    Go type_spec body (refines kind to struct/interface)
//	@import      an import specifier (string literal or dotted name)
//	@fn          callee of a call whose first string argument is @import (require)
//	@base        PHP: left side of BASE . 'path.php' in require/include

const goQuery = `
(function_declaration name: (identifier) @name) @def.function
(method_declaration receiver: (parameter_list) @recv name: (field_identifier) @name) @def.method
(type_spec name: (type_identifier) @name type: (_) @typebody) @def.type
(source_file (var_declaration (var_spec name: (identifier) @name) @def.var))
(source_file (var_declaration (var_spec_list (var_spec name: (identifier) @name) @def.var)))
(source_file (const_declaration (const_spec name: (identifier) @name) @def.const))
(import_spec path: (_) @import)
`

const pythonQuery = `
(function_definition name: (identifier) @name) @def.function
(class_definition name: (identifier) @name) @def.class
(import_statement name: (dotted_name) @import)
(import_statement name: (aliased_import name: (dotted_name) @import))
(import_from_statement module_name: (_) @import)
`

const javascriptQuery = `
(function_declaration name: (identifier) @name) @def.function
(generator_function_declaration name: (identifier) @name) @def.function
(class_declaration name: (identifier) @name) @def.class
(method_definition name: (_) @name) @def.method
(variable_declarator name: (identifier) @name value: [(arrow_function) (function_expression)]) @def.function
(import_statement source: (string) @import)
(export_statement source: (string) @import)
(call_expression function: (identifier) @fn arguments: (arguments . (string) @import))
`

const typescriptQuery = `
(function_declaration name: (identifier) @name) @def.function
(generator_function_declaration name: (identifier) @name) @def.function
(class_declaration name: (type_identifier) @name) @def.class
(abstract_class_declaration name: (type_identifier) @name) @def.class
(interface_declaration name: (type_identifier) @name) @def.interface
(type_alias_declaration name: (type_identifier) @name) @def.type
(enum_declaration name: (identifier) @name) @def.enum
(method_definition name: (_) @name) @def.method
(variable_declarator name: (identifier) @name value: [(arrow_function) (function_expression)]) @def.function
(import_statement source: (string) @import)
(export_statement source: (string) @import)
(call_expression function: (identifier) @fn arguments: (arguments . (string) @import))
`

const javaQuery = `
(class_declaration name: (identifier) @name) @def.class
(record_declaration name: (identifier) @name) @def.class
(interface_declaration name: (identifier) @name) @def.interface
(annotation_type_declaration name: (identifier) @name) @def.interface
(enum_declaration name: (identifier) @name) @def.enum
(method_declaration name: (identifier) @name) @def.method
(constructor_declaration name: (identifier) @name) @def.method
(import_declaration [(scoped_identifier) (identifier)] @import)
`

const phpQuery = `
(function_definition name: (name) @name) @def.function
(class_declaration name: (name) @name) @def.class
(trait_declaration name: (name) @name) @def.class
(interface_declaration name: (name) @name) @def.interface
(enum_declaration name: (name) @name) @def.enum
(method_declaration name: (name) @name) @def.method
(namespace_use_clause [(qualified_name) (name)] @import)
(require_expression [(string) (encapsed_string)] @import)
(require_once_expression [(string) (encapsed_string)] @import)
(include_expression [(string) (encapsed_string)] @import)
(include_once_expression [(string) (encapsed_string)] @import)
(require_once_expression (binary_expression left: (_) @base right: [(string) (encapsed_string)] @import))
(require_expression (binary_expression left: (_) @base right: [(string) (encapsed_string)] @import))
(include_once_expression (binary_expression left: (_) @base right: [(string) (encapsed_string)] @import))
(include_expression (binary_expression left: (_) @base right: [(string) (encapsed_string)] @import))
`
