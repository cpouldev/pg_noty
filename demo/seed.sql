CREATE TABLE public.orders (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    customer text NOT NULL,
    total_cents integer NOT NULL
);
