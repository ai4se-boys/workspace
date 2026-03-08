def debugLogAsJSON(frmt: str, *args: any) -> None:
    """Log debug information in JSON format when debug mode is enabled.

    This function logs formatted debug messages along with caller information
    and JSON-formatted data when the Debug flag is set to True. The last
    argument is marshaled into JSON format and appended to the log message.

    Args:
        frmt: The format string for the log message.
        *args: Variable length argument list. The last argument will be
               marshaled into JSON format, while preceding arguments are
               used for string formatting.

    Returns:
        None

    Raises:
        TypeError: If arguments cannot be marshaled to JSON.
        ValueError: If there are issues with string formatting.
    """