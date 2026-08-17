from argus.runtime.messages import (
    AudioPart,
    ImagePart,
    Message,
    MessageRole,
    TextPart,
    ToolCallPart,
    ToolResultPart,
)


def test_message_supports_provider_neutral_multimodal_parts() -> None:
    message = Message(
        role=MessageRole.USER,
        parts=(
            TextPart(text="Locate the submit button."),
            ImagePart(data=b"png", media_type="image/png"),
        ),
    )

    assert isinstance(message.parts[0], TextPart)
    assert isinstance(message.parts[1], ImagePart)
    assert message.parts[0].text == "Locate the submit button."
    assert message.parts[1].media_type == "image/png"


def test_tool_result_references_its_provider_neutral_call_id() -> None:
    call = ToolCallPart(
        call_id="call-1",
        name="read_page",
        arguments={"include_forms": True},
    )
    result = ToolResultPart(
        call_id=call.call_id,
        name=call.name,
        result={"visible_text": "Welcome"},
    )

    assert result.call_id == "call-1"
    assert result.result == {"visible_text": "Welcome"}


def test_message_supports_provider_neutral_audio_parts() -> None:
    part = AudioPart(data=b"mp3", media_type="audio/mpeg")

    assert part.data == b"mp3"
    assert part.media_type == "audio/mpeg"
